package supervisor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/go-logr/logr"
	"github.com/spf13/afero"
	"k8s.io/client-go/rest"

	"github.com/redpanda-data/common-go/rpadmin"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	rpkconfig "github.com/redpanda-data/redpanda/src/go/rpk/pkg/config"
)

type Supervisor struct {
	rpkPath      string
	arguments    []string
	outputStream io.Writer
	errorStream  io.Writer

	rpkYamlPath string

	logger logr.Logger

	shutdownTimeout time.Duration

	factory        internalclient.ClientFactory
	server         *http.Server
	cmd            *exec.Cmd
	running        atomic.Bool
	cmdExitedCh    chan error
	serverExitedCh chan error
}

type Config struct {
	ExecutablePath   string
	Arguments        []string
	OutputStream     io.Writer
	ErrorStream      io.Writer
	ShutdownTimeout  time.Duration
	RedpandaYamlPath string
	Port             int
	Logger           logr.Logger
}

func New(config Config) (*Supervisor, error) {
	var err error

	path := config.ExecutablePath
	if path == "" {
		path, err = exec.LookPath("rpk")
		if err != nil {
			return nil, fmt.Errorf("unable to find rpk binary on in system path %w", err)
		}
	}

	logger := config.Logger
	if logger.IsZero() {
		logger = logr.Discard()
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("unable to find rpk binary at %q", path)
		}
		return nil, fmt.Errorf("unable to check rpk binary: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("path to rpk binary should be file, current path %q is a directory", path)
	}
	if !info.Mode().IsRegular() || (info.Mode()&0111 == 0) {
		return nil, fmt.Errorf("rpk binary %q is not executable", path)
	}

	rpkYamlPath := config.RedpandaYamlPath
	if rpkYamlPath == "" {
		rpkYamlPath = "/etc/redpanda/redpanda.yaml"
	}

	info, err = os.Stat(rpkYamlPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("unable to find rpk.yaml at %q", path)
		}
		return nil, fmt.Errorf("unable to check rpk.yaml: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("path to rpk.yaml should be file, current path %q is a directory", path)
	}

	outputStream := config.OutputStream
	if outputStream == nil {
		outputStream = os.Stdout
	}

	errorStream := config.ErrorStream
	if errorStream == nil {
		errorStream = os.Stderr
	}

	shutdownTimeout := config.ShutdownTimeout
	if shutdownTimeout == 0 {
		shutdownTimeout = 5 * time.Second
	}

	port := config.Port
	if port == 0 {
		port = 9999
	}

	supervisor := &Supervisor{
		shutdownTimeout: shutdownTimeout,
		rpkYamlPath:     rpkYamlPath,
		rpkPath:         path,
		arguments:       config.Arguments,
		outputStream:    outputStream,
		errorStream:     errorStream,
		logger:          logger,
		cmdExitedCh:     make(chan error),
		serverExitedCh:  make(chan error),
		factory:         internalclient.NewFactory(&rest.Config{}, nil),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", supervisor.HandleHealthyCheck)
	mux.HandleFunc("/readyz", supervisor.HandleReadyCheck)

	supervisor.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	return supervisor, nil
}

func (s *Supervisor) HandleHealthyCheck(w http.ResponseWriter, r *http.Request) {
	healthy, err := s.IsNodeHealthy(r.Context())
	if err != nil {
		s.logger.Error(err, "error running health check")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if healthy {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusBadRequest)
}

func (s *Supervisor) HandleReadyCheck(w http.ResponseWriter, r *http.Request) {
	if s.IsNodeReady(r.Context()) {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusBadRequest)
}

func (s *Supervisor) Run(ctx context.Context) error {
	if !s.running.CompareAndSwap(false, true) {
		return errors.New("supervisor already initialized")
	}

	s.initializeStartCommand(ctx)

	s.logger.V(2).Info("running rpk", "command", strings.Join(s.cmd.Args, " "))
	if err := s.cmd.Start(); err != nil {
		return fmt.Errorf("running rpk: %w", err)
	}

	var wg sync.WaitGroup

	wg.Add(2)

	// This goroutine is responsible for waiting on the process, triggering cleanup,
	// and notifying the caller that the process has exited.
	go func() {
		defer wg.Done()

		err := s.cmd.Wait()
		s.logger.Info("rpk process exited", "error", err)
		s.cmdExitedCh <- err
		close(s.cmdExitedCh)
	}()

	shutdownServer := func() error {
		ctx, cancel := context.WithTimeout(ctx, s.shutdownTimeout)
		defer cancel()

		return s.server.Shutdown(ctx)
	}

	shutdownCmd := func() error {
		if err := s.quitCommand(); err != nil {
			return errors.Join(err, s.killCommand())
		}
		return nil
	}

	// This goroutine is responsible for starting the server.
	go func() {
		defer wg.Done()

		if err := s.server.ListenAndServe(); err != nil {
			if !errors.Is(err, http.ErrServerClosed) {
				s.logger.Info("server exited", "error", err)
				s.serverExitedCh <- err
			}
			close(s.serverExitedCh)
		}
	}()

	var err error

	select {
	case err = <-s.cmdExitedCh:
		err = errors.Join(err, shutdownServer())
	case err = <-s.serverExitedCh:
		err = errors.Join(err, shutdownCmd())
	case <-ctx.Done():
		err = errors.Join(shutdownServer(), shutdownCmd())
	}

	wg.Wait()

	return err
}

func (s *Supervisor) IsNodeReady(ctx context.Context) bool {
	if !s.running.Load() {
		s.logger.Info("waiting for supervisor to start rpk binary")
		return false
	}

	if err := s.runReadyCheck(ctx); err != nil {
		s.logger.Error(err, "ready check failed")
		return false
	}

	return true
}

func (s *Supervisor) IsNodeHealthy(ctx context.Context) (bool, error) {
	if !s.running.Load() {
		return false, nil
	}

	healthy, err := s.runHealthCheck(ctx)
	if err != nil {
		s.logger.Error(err, "health check failed")
		return false, err
	}

	return healthy, nil
}

func (s *Supervisor) initializeStartCommand(ctx context.Context) {
	s.cmd = exec.CommandContext(ctx, s.rpkPath, append(
		[]string{
			"redpanda", "start",
			"--config", s.rpkYamlPath,
		},
		s.arguments...,
	)...)
	s.cmd.Stdout = s.outputStream
	s.cmd.Stderr = s.errorStream

	// Start rpk in its own process group to avoid directly receiving
	// SIGTERM intended for the supervisor, let the supervisor handle
	// graceful shutdown.
	s.cmd.SysProcAttr = getProcessAttr()
}

func (s *Supervisor) quitCommand() error {
	if !s.running.Load() {
		return errors.New("waiting for supervisor to start rpk binary")
	}

	select {
	case err := <-s.cmdExitedCh:
		return err
	default:
	}

	s.logger.V(2).Info("gracefully shutting down rpk process")

	if err := s.runStopCommand(s.shutdownTimeout); err != nil {
		return fmt.Errorf("error running rpk redpanda stop command")
	}

	return nil
}

func (s *Supervisor) killCommand() error {
	if !s.running.Load() {
		return errors.New("waiting for supervisor to start rpk binary")
	}

	select {
	case err := <-s.cmdExitedCh:
		return err
	default:
	}

	s.logger.V(2).Info("killing rpk process")

	if err := s.cmd.Process.Kill(); err != nil {
		return fmt.Errorf("error kill rpk redpanda process")
	}

	return nil
}

func (s *Supervisor) runReadyCheck(ctx context.Context) error {
	client, err := s.getClient(ctx)
	if err != nil {
		return err
	}

	// just check the health endpoint to make sure that the server
	// is responsive
	_, err = client.GetHealthOverview(ctx)
	return err
}

func (s *Supervisor) runHealthCheck(ctx context.Context) (bool, error) {
	client, err := s.getClient(ctx)
	if err != nil {
		return false, err
	}

	health, err := client.GetHealthOverview(ctx)
	if err != nil {
		return false, err
	}

	// TODO: implement extended health check
	return health.IsHealthy, nil
}

func (s *Supervisor) runStopCommand(timeout time.Duration) error {
	cmd := exec.Command(s.rpkPath, []string{
		"redpanda", "stop",
		"--config", s.rpkYamlPath,
		"--timeout", timeout.String(),
	}...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard

	return cmd.Run()
}

func (s *Supervisor) getClient(ctx context.Context) (*rpadmin.AdminAPI, error) {
	params := rpkconfig.Params{ConfigFlag: s.rpkYamlPath}

	config, err := params.Load(afero.NewOsFs())
	if err != nil {
		return nil, err
	}

	return s.factory.RedpandaAdminClient(ctx, config.VirtualProfile())
}

func getProcessAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setpgid: true,
	}
}
