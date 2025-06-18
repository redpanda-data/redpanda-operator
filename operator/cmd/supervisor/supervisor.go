package supervisor

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/time/rate"

	"github.com/redpanda-data/redpanda-operator/pkg/otelutil/log"
)

func Command() *cobra.Command {
	var retryBurst int
	var retryRate time.Duration
	var gracePeriod time.Duration

	cmd := &cobra.Command{
		Use:     "supervisor command...",
		Example: "supervisor --burst-rate=1m -- redpanda-operator sidecar --redpanda-yaml path/on/disk",
		Short:   "Repeatedly run a given command, akin the supervisord",
		Long: `supervisor is a stand in for Kubernetes' Sidecar feature.
It continuously re-runs the provided command, forwarding signals, in a loop until SIGINT is received.
Subprocess retries are rate limited with token bucket that may be tuned via the --retry-burst and --retry-rate flags.
The default retry parameters are tuned for a a long running process, such as the sidecar subcommand.`,
		Args: cobra.MinimumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			// As noted by Notify, we're subscribing to all signals and
			// therefore buffering once per signal for a total of 16.
			signals := make(chan os.Signal, 16)

			// Looks a bit strange but we're subscribing to all signals _but_
			// SIGINT and SIGTERM. We reserve those signals for initiating the
			// graceful shutdown process.
			signal.Notify(signals)
			signal.Reset(os.Interrupt, syscall.SIGTERM)

			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer cancel()

			limiter := rate.NewLimiter(rate.Every(retryRate), retryBurst)
			runner := Runner{
				Command:     args,
				GracePeriod: gracePeriod,
				Signals:     signals,
				Stdout:      os.Stdout,
				Stderr:      os.Stderr,
			}

			// Loop forever, re-running our child process until the parent
			// process receives a request to shutdown.
			for {
				// We start by consuming a retry "credit" for correctness but
				// also to fail quickly if the provided retry parameters result
				// in an unusable limiter.
				reservation := limiter.Reserve()

				// The API and docs are a bit confusing here as they mention
				// parameters that's not exposed. OK indicates whether or not
				// the reservation COULD be granted not if it was. i.e. OK will
				// be false if retryBurst is < 1.
				if !reservation.OK() {
					panic(fmt.Sprintf("invalid retry parameters:\n--retry-burst=%d\n--retry-rate=%s", retryBurst, retryRate))
				}

				delay := reservation.Delay()

				if delay > 0 {
					log.Info(ctx, "backing off retries", "delay", delay.String())
				}

				// Otherwise wait until we have a retry "credit" available.
				select {
				case <-ctx.Done():
					log.Info(ctx, "shutting down")
					return

				case <-time.After(reservation.Delay()):
				}

				if !runner.Run(ctx) {
					return
				}
			}
		},
	}

	cmd.Flags().IntVar(&retryBurst, "retry-burst", 3, "The `burst` parameter of the token bucket retry. Governs the maximum number of retries before initiating a back off.")
	cmd.Flags().DurationVar(&retryRate, "retry-rate", 3*time.Minute, "The `rate` parameter of the token bucket retry. Controls the period at which retries attempts are permitted and represents the maximum back off.")
	cmd.Flags().DurationVar(&gracePeriod, "grace-period", 10*time.Second, "The duration to wait for child processes to terminate upon receiving SIGINT before SIGKILL'ing them. Should be less than the grace period of the Container running this command.")

	return cmd
}

type Runner struct {
	Command     []string
	GracePeriod time.Duration
	Signals     <-chan os.Signal
	Stdout      io.Writer
	Stderr      io.Writer
}

func (r *Runner) Run(
	ctx context.Context,
) (cont bool) {
	log.Info(ctx, "starting process", "command", r.Command)

	//nolint:gosec // This is running with a users permissions, nothing to exploit here.
	subprocess := exec.Command(r.Command[0], r.Command[1:]...)

	// Forward all output to this process'
	subprocess.Stderr = r.Stderr
	subprocess.Stdout = r.Stdout

	if err := subprocess.Start(); err != nil {
		log.Error(ctx, err, "failed to start process", "command", r.Command)
		// If we fail to startup the provided process, don't continue to retry
		// as it's exceptionally unlikely this type of issue will resolve
		// itself.
		return false
	}

	doneCh := make(chan error, 1)
	go func() {
		doneCh <- subprocess.Wait()
	}()

	for {
		select {
		case err := <-doneCh:
			log.Error(ctx, err, "process exited")
			return true // Continue to re-run on any process crashes.

		case sig := <-r.Signals:
			// Forward signals onto our child process.
			log.Info(ctx, "forwarding signal", "signal", sig)
			if err := subprocess.Process.Signal(sig); err != nil {
				log.Error(ctx, err, "failed to forward signal to subprocess", "signal", sig)
			}
			continue

		case <-ctx.Done():
			log.Info(ctx, "terminating process")

			// Initiate the shutdown process by sending SIGTERM to match
			// Kubernetes' behavior[1] (in most cases).
			// We fire and forget because the process is going to get killed
			// one way or another.
			// [1]: https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#pod-termination-stop-signals
			_ = subprocess.Process.Signal(syscall.SIGTERM)

			// Wait for at most gracePeriod for the process to exit gracefully
			// before KILL'ing it.
			select {
			case <-doneCh:
				log.Info(ctx, "process gracefully terminated")
			case <-time.After(r.GracePeriod):
				_ = subprocess.Process.Kill()
				log.Info(ctx, "process killed")
			}

			// If the context has been cancelled, don't continue the loop.
			return false
		}
	}
}
