// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package kube

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/cockroachdb/errors"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/transport/spdy"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redpanda-data/redpanda-operator/pkg/otelutil/log"
)

type (
	Object     = client.Object
	ObjectList = client.ObjectList
	ObjectKey  = client.ObjectKey

	InNamespace = client.InNamespace
)

type Option interface {
	ApplyToOptions(*Options)
}

type Options struct {
	client.Options

	FieldManager string
}

func (o Options) ApplyToOptions(opts *Options) {
	if o.Cache != nil {
		opts.Cache = o.Cache
	}

	if o.Scheme != nil {
		opts.Scheme = o.Scheme
	}

	if o.DryRun != nil {
		opts.DryRun = o.DryRun
	}

	if o.HTTPClient != nil {
		opts.HTTPClient = o.HTTPClient
	}

	if o.Mapper != nil {
		opts.Mapper = o.Mapper
	}

	if o.HTTPClient != nil {
		opts.HTTPClient = o.HTTPClient
	}
}

// FromEnv returns a [Ctl] for the default context in $KUBECONFIG.
func FromEnv(opts ...Option) (*Ctl, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
	config, err := kubeConfig.ClientConfig()
	if err != nil {
		return nil, err
	}

	return FromRESTConfig(config, opts...)
}

func FromConfig(cfg Config, opts ...Option) (*Ctl, error) {
	rest, err := ConfigToRest(cfg)
	if err != nil {
		return nil, err
	}
	return FromRESTConfig(rest, opts...)
}

func FromRESTConfig(cfg *RESTConfig, opts ...Option) (*Ctl, error) {
	var options Options
	for _, o := range opts {
		o.ApplyToOptions(&options)
	}

	c, err := client.New(cfg, options.Options)
	if err != nil {
		return nil, err
	}

	fieldManager := options.FieldManager
	if fieldManager == "" {
		fieldManager = "*kube.Ctl"
	}

	return &Ctl{config: cfg, client: c, fieldManager: fieldManager}, nil
}

// Ctl is a Kubernetes client inspired by the shape of the `kubectl` CLI with a
// focus on being ergonomic.
type Ctl struct {
	config       *rest.Config
	client       client.Client
	fieldManager string
}

// RestConfig returns a deep copy of the [rest.Config] used by this [Ctl].
func (c *Ctl) RestConfig() *rest.Config {
	return rest.CopyConfig(c.config)
}

// Get fetches the latest state of an object into `obj` from Kubernetes.
// Usage:
//
//	var pod corev1.Pod
//	ctl.Get(ctx, kube.ObjectKey{Namespace: "", Name:""}, &pod)
func (c *Ctl) Get(ctx context.Context, key ObjectKey, obj Object) error {
	if err := c.client.Get(ctx, key, obj); err != nil {
		return errors.WithStack(err)
	}
	return nil
}

// GetAndWait is the equivalent of calling [Ctl.Get] followed by [Ctl.WaitFor].
func (c *Ctl) GetAndWait(ctx context.Context, key ObjectKey, obj Object, cond CondFn[Object]) error {
	if err := c.Get(ctx, key, obj); err != nil {
		return err
	}
	return c.WaitFor(ctx, obj, cond)
}

// List fetches a list of objects into `objs` from Kubernetes.
// Usage:
//
//	var pods corev1.PodList
//	ctl.List(ctx, &pods)
func (c *Ctl) List(ctx context.Context, objs client.ObjectList, opts ...client.ListOption) error {
	if err := c.client.List(ctx, objs, opts...); err != nil {
		return errors.WithStack(err)
	}
	return nil
}

// Apply "applies" the provided [Object] via SSA (Server Side Apply).
func (c *Ctl) Apply(ctx context.Context, obj Object) error {
	kinds, _, err := c.client.Scheme().ObjectKinds(obj)
	if err != nil {
		return errors.WithStack(err)
	}

	obj.SetManagedFields(nil)

	obj.GetObjectKind().SetGroupVersionKind(kinds[0])

	if err := c.client.Patch(ctx, obj, client.Apply, client.FieldOwner(c.fieldManager)); err != nil {
		return errors.WithStack(err)
	}

	return nil
}

// ApplyAndWait is the equivalent of calling [Ctl.Apply] followed by [Ctl.WaitFor].
func (c *Ctl) ApplyAndWait(ctx context.Context, obj Object, cond CondFn[Object]) error {
	if err := c.Apply(ctx, obj); err != nil {
		return err
	}

	return c.WaitFor(ctx, obj, cond)
}

// ApplyAll "applies" the all provided [Object] via SSA (Server Side Apply).
// Individual failures do not abort the entire operation; an aggregated error,
// if any, is returned.
func (c *Ctl) ApplyAll(ctx context.Context, objs ...Object) error {
	var errs []error
	for _, obj := range objs {
		if err := c.Apply(ctx, obj); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// ApplyAllAndWait is the equivalent of calling [Ctl.ApplyAll] followed by
// [Ctl.WaitFor] in a loop.
//
// If ApplyAll fails, the entire wait loop is aborted.
//
// Individual failures in the wait loop do not abort the entire operator; an
// aggregated error, if any, is returned.
func (c *Ctl) ApplyAllAndWait(ctx context.Context, cond CondFn[Object], objs ...Object) error {
	if err := c.ApplyAll(ctx, objs...); err != nil {
		return err
	}

	var errs []error
	for _, obj := range objs {
		if err := c.WaitFor(ctx, obj, cond); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Create creates the given [Object].
func (c *Ctl) Create(ctx context.Context, obj Object) error {
	if err := c.client.Create(ctx, obj); err != nil {
		return errors.WithStack(err)
	}
	return nil
}

// CreateAndWait is the equivalent of calling [Ctl.Create] followed by [Ctl.WaitFor].
func (c *Ctl) CreateAndWait(ctx context.Context, obj Object, cond CondFn[Object]) error {
	if err := c.Create(ctx, obj); err != nil {
		return err
	}
	return c.WaitFor(ctx, obj, cond)
}

// Delete initiates the deletion the given [Object].
func (c *Ctl) Delete(ctx context.Context, obj Object) error {
	if err := c.client.Delete(ctx, obj); err != nil {
		return errors.WithStack(err)
	}
	return nil
}

// DeleteAndWait is the equivalent of calling [Ctl.Delete] followed by
// [Ctl.WaitFor] with [IsDeleted].
func (c *Ctl) DeleteAndWait(ctx context.Context, obj Object) error {
	if err := c.Delete(ctx, obj); err != nil {
		return err
	}

	// Wait for the Object to be removed from the API server.
	return c.WaitFor(ctx, obj, IsDeleted[Object])
}

// CondFn is a condition checker for Kubernetes Objects. The provided error is
// the result of [Ctl.Get] and may be used e.g. to await 404's in Deletes.
type CondFn[T Object] func(T, error) (bool, error)

// IsDeleted is a [CondFn] that returns true when the err is a 404.
func IsDeleted[T Object](obj T, err error) (bool, error) {
	if k8serrors.IsNotFound(err) {
		return true, nil
	}
	return false, err
}

// WaitFor blocks until `cond` returns true for obj or ctx is cancelled. obj is
// continuously refreshed via [Ctl.Get] before calling cond. If ctx does not
// have a deadline a default of 5m will be used.
func (c *Ctl) WaitFor(ctx context.Context, obj Object, cond CondFn[Object]) error {
	const timeout = 5 * time.Minute
	const logEvery = 10 * time.Second

	// If ctx doesn't have a deadline, we'll apply the default.
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	start := time.Now()
	lastLog := start

	// TODO(chrisseto): We should be able to pull this off obj but Get doesn't
	// seem to set TypeMeta?
	kinds, _, err := c.client.Scheme().ObjectKinds(obj)
	if err != nil {
		return errors.WithStack(err)
	}

	gvk := kinds[0]

	for {
		err := c.Get(ctx, AsKey(obj), obj)

		done, err := cond(obj, err)
		if err != nil {
			return err
		}

		if done {
			log.Info(ctx, "Cond satisfied", "key", AsKey(obj), "gvk", gvk, "waited", time.Since(start))
			return nil
		}

		if time.Since(lastLog) >= logEvery {
			lastLog = time.Now()
			log.Info(ctx, "waiting for Cond", "key", AsKey(obj), "gvk", gvk, "waited", time.Since(start))
		}

		select {
		case <-time.After(10 * time.Second):
			continue
		case <-ctx.Done():
			return errors.WithStack(ctx.Err())
		}
	}
}

type ExecOptions struct {
	Container string
	Command   []string
	Stdin     io.Reader
	Stdout    io.Writer
	Stderr    io.Writer
}

// Exec runs `kubectl exec` on the given Pod in the style of [exec.Command].
func (c *Ctl) Exec(ctx context.Context, pod *corev1.Pod, opts ExecOptions) error {
	if opts.Container == "" {
		opts.Container = pod.Spec.Containers[0].Name
	}

	// Apparently, nothing in the k8s SDK, except exec'ing, uses RESTClientFor.
	// RESTClientFor checks for GroupVersion and NegotiatedSerializer which are
	// never set by the config loading tool chain.
	// The .APIPath setting was a random shot in the dark that happened to work...
	// Pulled from https://github.com/kubernetes/kubectl/blob/acf4a09f2daede8fdbf65514ade9426db0367ed3/pkg/cmd/util/kubectl_match_version.go#L115
	cfg := c.RestConfig()
	cfg.APIPath = "/api"
	cfg.GroupVersion = &schema.GroupVersion{Version: "v1"}
	cfg.NegotiatedSerializer = scheme.Codecs.WithoutConversion()
	restClient, err := rest.RESTClientFor(cfg)
	if err != nil {
		return errors.WithStack(err)
	}

	// Inspired by https://github.com/kubernetes/kubectl/blob/acf4a09f2daede8fdbf65514ade9426db0367ed3/pkg/cmd/exec/exec.go#L388
	req := restClient.Post().
		Resource("pods").
		Name(pod.Name).
		Namespace(pod.Namespace).
		SubResource("exec")

	req.VersionedParams(&corev1.PodExecOptions{
		Container: opts.Container,
		Command:   opts.Command,
		Stdin:     opts.Stdin != nil,
		Stdout:    opts.Stdout != nil,
		Stderr:    opts.Stderr != nil,
		TTY:       false,
	}, runtime.NewParameterCodec(c.client.Scheme()))

	// TODO(chrisseto): SPDY is reported to be deprecated but
	// NewWebSocketExecutor doesn't appear to work in our version of KinD.
	exec, err := remotecommand.NewSPDYExecutor(c.config, "POST", req.URL())
	// exec, err := remotecommand.NewWebSocketExecutor(c.config, "GET", req.URL().String())
	if err != nil {
		return errors.WithStack(err)
	}

	return exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stderr: opts.Stderr,
		Stdout: opts.Stdout,
		Stdin:  opts.Stdin,
	})
}

func (c *Ctl) PortForward(ctx context.Context, pod *corev1.Pod, out, errOut io.Writer) ([]portforward.ForwardedPort, func(), error) {
	// Apparently, nothing in the k8s SDK, except exec'ing, uses RESTClientFor.
	// RESTClientFor checks for GroupVersion and NegotiatedSerializer which are
	// never set by the config loading tool chain.
	// The .APIPath setting was a random shot in the dark that happened to work...
	// Pulled from https://github.com/kubernetes/kubectl/blob/acf4a09f2daede8fdbf65514ade9426db0367ed3/pkg/cmd/util/kubectl_match_version.go#L115
	cfg := c.RestConfig()
	cfg.APIPath = "/api"
	cfg.GroupVersion = &schema.GroupVersion{Version: "v1"}
	cfg.NegotiatedSerializer = scheme.Codecs.WithoutConversion()
	restClient, err := rest.RESTClientFor(cfg)
	if err != nil {
		return nil, nil, errors.WithStack(err)
	}

	// Inspired by https://github.com/kubernetes/kubectl/blob/acf4a09f2daede8fdbf65514ade9426db0367ed3/pkg/cmd/portforward/portforward.go#L410-L416
	req := restClient.Post().
		Resource("pods").
		Namespace(pod.Namespace).
		Name(pod.Name).
		SubResource("portforward")

	transport, upgrader, err := spdy.RoundTripperFor(cfg)
	if err != nil {
		return nil, nil, errors.WithStack(err)
	}

	stopChan := make(chan struct{})
	readyChan := make(chan struct{})

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", req.URL())

	var ports []string
	for _, container := range pod.Spec.Containers {
		for _, port := range container.Ports {
			// port forward and spdy does not handle UDP connection correctly
			//
			// Reference
			// https://github.com/kubernetes/kubernetes/issues/47862
			// https://github.com/kubernetes/kubectl/blob/acf4a09f2daede8fdbf65514ade9426db0367ed3/pkg/cmd/portforward/portforward.go#L273-L290
			if port.Protocol != corev1.ProtocolTCP {
				continue
			}

			ports = append(ports, fmt.Sprintf(":%d", port.ContainerPort))
		}
	}

	fw, err := portforward.New(dialer, ports, stopChan, readyChan, out, errOut)
	if err != nil {
		return nil, nil, errors.WithStack(err)
	}

	go func() {
		err = fw.ForwardPorts()
		if err != nil {
			fmt.Fprintf(errOut, "failed while forwaring ports: %v\n", err)
		}
	}()

	select {
	case <-fw.Ready:
	case <-ctx.Done():
	}

	p, err := fw.GetPorts()
	if err != nil {
		return nil, nil, errors.WithStack(err)
	}

	return p, func() {
		if stopChan != nil {
			close(stopChan)
		}
	}, nil
}
