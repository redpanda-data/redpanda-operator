// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package testenv

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/redpanda-data/common-go/kube"
	"github.com/redpanda-data/common-go/otelutil/otelkube"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8sapierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	goclientscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/pkg/k3d"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
	"github.com/redpanda-data/redpanda-operator/pkg/vcluster"
)

type Env struct {
	t                  *testing.T
	ctx                context.Context
	cancel             context.CancelFunc
	namespace          *corev1.Namespace
	logger             logr.Logger
	scheme             *runtime.Scheme
	group              *errgroup.Group
	host               *k3d.Cluster
	config             *rest.Config
	client             client.Client
	watchAllNamespaces bool
	Name               string

	recordersMu sync.Mutex
	recorders   map[string]*podLogRecorder
}

type Options struct {
	Name                string
	Agents              int
	SkipVCluster        bool
	SkipNamespaceClient bool
	// WatchAllNamespaces makes the controller manager watch all namespaces
	// instead of just the test namespace. This enables parallel tests that
	// each create their own namespace via [Env.CreateTestNamespace].
	WatchAllNamespaces bool
	Scheme             *runtime.Scheme
	CRDs               []*apiextensionsv1.CustomResourceDefinition
	Logger             logr.Logger
	Network            string
	Namespace          string
	ImportImages       []string
	Domain             string
	Port               int
	PortMappings       []k3d.PortMapping
}

// New returns a configured [Env] that utilizes an [vcluster.Cluster] in a
// shared k3d cluster.
//
// Due to the shared nature, the k3d cluster will NOT be shutdown at the end of
// tests. The vCluster will be deleted unless -retain is specified.
func New(t *testing.T, options Options) *Env {
	t.Helper()

	if options.Agents == 0 {
		options.Agents = 3
	}

	if options.Name == "" {
		options.Name = k3d.SharedClusterName
	}

	if options.Scheme == nil {
		options.Scheme = goclientscheme.Scheme
	}

	if options.Logger.IsZero() {
		options.Logger = logr.Discard()
	}

	opts := []k3d.ClusterOpt{k3d.WithAgents(options.Agents)}

	if options.Network != "" {
		opts = append(opts, k3d.WithNetwork(options.Network))
	}
	if options.Domain != "" {
		opts = append(opts, k3d.WithDomain(options.Domain))
	}
	if options.Port != 0 {
		opts = append(opts, k3d.WithPort(options.Port))
	}
	opts = append(opts, k3d.WithMappedPorts(options.PortMappings...))

	host, err := k3d.GetOrCreate(options.Name, opts...)
	require.NoError(t, err)

	if len(options.ImportImages) > 0 {
		options.Logger.Info("importing images", "count", len(options.ImportImages))
		require.NoError(t, host.ImportImage(options.ImportImages...))
	}

	ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec

	var cluster *vcluster.Cluster
	config := host.RESTConfig()

	if !options.SkipVCluster {
		cluster, err = vcluster.New(ctx, host.RESTConfig())
		require.NoError(t, err)
		config = cluster.RESTConfig()
	}

	if len(options.CRDs) > 0 {
		// When running with -p=N, multiple test packages install CRDs into the
		// same shared cluster concurrently. envtest.InstallCRDs aborts at the
		// first create/update conflict without touching the CRDs later in the
		// list, so a single tolerated failure can leave those CRDs missing for
		// the rest of the run. Retry until a full pass installs and waits for
		// every CRD.
		var crds []*apiextensionsv1.CustomResourceDefinition
		err := retry.OnError(
			wait.Backoff{Steps: 10, Duration: 500 * time.Millisecond, Factor: 1.5, Cap: 5 * time.Second},
			func(err error) bool {
				return k8sapierrors.IsAlreadyExists(err) || k8sapierrors.IsConflict(err) || strings.Contains(err.Error(), "already exists")
			},
			func() error {
				var err error
				crds, err = envtest.InstallCRDs(config, envtest.CRDInstallOptions{
					CRDs:               dupCRDs(options.CRDs),
					ErrorIfPathMissing: false,
				})
				return err
			},
		)
		require.NoError(t, err)
		require.Equal(t, len(options.CRDs), len(crds))
	}

	c, err := client.New(config, client.Options{Scheme: options.Scheme})
	require.NoError(t, err)
	g, ctx := errgroup.WithContext(ctx)

	// Create a unique Namespace to perform tests within.
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		GenerateName: "testenv-",
	}}

	if options.Namespace != "" {
		ns = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
			Name: options.Namespace,
		}}
	}
	createErr := c.Create(ctx, ns)
	if !k8sapierrors.IsAlreadyExists(createErr) {
		require.NoError(t, createErr)
	}

	var otelClient client.Client
	if options.SkipNamespaceClient {
		otelClient = otelkube.NewClient(c)
	} else {
		otelClient = otelkube.NewClient(client.NewNamespacedClient(c, ns.Name))
	}

	env := &Env{
		t:                  t,
		scheme:             options.Scheme,
		logger:             options.Logger,
		namespace:          ns,
		group:              g,
		ctx:                ctx,
		cancel:             cancel,
		host:               host,
		config:             config,
		client:             otelClient,
		watchAllNamespaces: options.WatchAllNamespaces,
		Name:               options.Name,
	}

	if !options.SkipVCluster {
		t.Logf("Executing in namespace '%s' of vCluster '%s'", ns.Name, cluster.Name())
		t.Logf("Connect to vCluster using 'vcluster connect --namespace %s %s -- bash'", cluster.Name(), cluster.Name())
	} else {
		t.Logf("Executing in namespace '%s'", ns.Name)
	}

	env.recordLogs(t, ns.Name)

	t.Cleanup(func() {
		// Dump diagnostics before cleanup if the test failed.
		if t.Failed() {
			env.diagnosticsFor(t, ns.Name).dump()
		}

		env.cancel()
		assert.NoError(env.t, env.group.Wait())

		if !testutil.Retain() {
			if !options.SkipVCluster {
				require.NoError(t, cluster.Delete())
			}

			// Clean up any clusters that aren't shared.
			if env.host.Name != k3d.SharedClusterName {
				require.NoError(t, env.host.Cleanup())
			}
		}
	})

	return env
}

func (e *Env) Client() client.Client {
	return e.client
}

func (e *Env) RESTConfig() *rest.Config {
	return e.config
}

func (e *Env) Host() *k3d.Cluster {
	return e.host
}

func (e *Env) Namespace() string {
	return e.namespace.Name
}

// TestNamespace represents an isolated namespace for a single test, with its
// own namespace-scoped client.
type TestNamespace struct {
	Name   string
	Client client.Client
}

// CreateTestNamespace creates a new isolated namespace for a single test and
// returns a namespace-scoped client. The namespace is deleted when the test
// completes (unless -retain is set). This enables parallel test execution
// within a shared testenv.
func (e *Env) CreateTestNamespace(t *testing.T) *TestNamespace {
	t.Helper()

	ctx := t.Context()

	// Create a unique namespace for this test.
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		GenerateName: "testenv-",
	}}
	// Use a non-namespaced client for namespace creation.
	rawClient, err := client.New(e.config, client.Options{Scheme: e.scheme})
	require.NoError(t, err)
	require.NoError(t, rawClient.Create(ctx, ns))

	t.Logf("Created test namespace %q", ns.Name)

	nsClient := otelkube.NewClient(client.NewNamespacedClient(rawClient, ns.Name))

	e.recordLogs(t, ns.Name)

	t.Cleanup(func() {
		if t.Failed() {
			e.diagnosticsFor(t, ns.Name).dump()
		}
		if !testutil.Retain() {
			if err := rawClient.Delete(context.Background(), ns); err != nil {
				t.Logf("WARNING: failed to delete namespace %s: %v", ns.Name, err)
			}
		}
	})

	return &TestNamespace{
		Name:   ns.Name,
		Client: nsClient,
	}
}

func (e *Env) SetupMulticlusterManager(serviceAccount string, address string, peers []multicluster.RaftCluster, fn func(multicluster.Manager) error) {
	// Bind the managers base config to a ServiceAccount via the "Impersonate"
	// feature. This ensures that any permissions/RBAC issues get caught by
	// theses tests as e.config has Admin permissions.
	config := rest.CopyConfig(e.config)
	if serviceAccount != "" {
		config.Impersonate.UserName = fmt.Sprintf("system:serviceaccount:%s:%s", e.Namespace(), serviceAccount)
	}

	manager, err := multicluster.NewRaftRuntimeManager(&multicluster.RaftConfiguration{
		Name:               e.Name,
		Address:            address,
		Peers:              peers,
		RestConfig:         config,
		Scheme:             e.scheme,
		Logger:             e.logger,
		Insecure:           true,
		SkipNameValidation: true,
		ElectionTimeout:    1 * time.Second,
		HeartbeatInterval:  100 * time.Millisecond,
		BaseContext: func() context.Context {
			return e.ctx
		},
	})
	require.NoError(e.t, err)
	require.NoError(e.t, fn(manager))

	e.group.Go(func() error {
		if err := manager.Start(e.ctx); err != nil && e.ctx.Err() != nil {
			return err
		}
		return nil
	})
}

func (e *Env) SetupManager(serviceAccount string, fn func(multicluster.Manager) error) {
	// Bind the managers base config to a ServiceAccount via the "Impersonate"
	// feature. This ensures that any permissions/RBAC issues get caught by
	// theses tests as e.config has Admin permissions.
	config := rest.CopyConfig(e.config)
	if serviceAccount != "" {
		config.Impersonate.UserName = fmt.Sprintf("system:serviceaccount:%s:%s", e.Namespace(), serviceAccount)
	}

	// TODO: Webhooks likely aren't going to place nicely with this method of
	// testing. The Kube API server will have to dial out of the cluster to the
	// local machine which could prove to be difficult across all docker/docker
	// in docker environments.
	// See also https://k3d.io/v5.4.6/faq/faq/?h=host#how-to-access-services-like-a-database-running-on-my-docker-host-machine
	cacheOpts := cache.Options{}
	if !e.watchAllNamespaces {
		// Limit this manager to only interacting with objects within our
		// namespace.
		cacheOpts.DefaultNamespaces = map[string]cache.Config{
			e.namespace.Name: {},
		}
	}

	manager, err := multicluster.NewSingleClusterManager(config, ctrl.Options{
		Cache:   cacheOpts,
		Metrics: server.Options{BindAddress: "0"}, // Disable metrics server to avoid port conflicts.
		Scheme:  e.scheme,
		Logger:  e.logger,
		BaseContext: func() context.Context {
			return e.ctx
		},
	})
	require.NoError(e.t, err)

	require.NoError(e.t, fn(manager))

	e.group.Go(func() error {
		if err := manager.Start(e.ctx); err != nil && e.ctx.Err() != nil {
			return err
		}
		return nil
	})

	// No Without leader election enabled, this is just a wait for the manager
	// to start up.
	<-manager.Elected()
}

// diagnosticsLogTailLines bounds every log tail captured on failure: enough
// to see a broker's last state transitions without flooding the test output.
const diagnosticsLogTailLines = 100

// nodeHealthLogPattern picks the kubelet and node-controller lines out of a
// k3d node container's k3s log: readiness transitions, status/lease posting
// and taint-based eviction. That log is the only place the reason for a
// NotReady transition is recorded.
var nodeHealthLogPattern = regexp.MustCompile(`(?i)nodenotready|nodeready|node status|not ready|pleg|lease|evict|taint|oom|deadline exceeded`)

// diagnostics dumps the state relevant to a failed test for one cluster: the
// test namespace's pods, events and workload objects, the cluster's node
// conditions and node events, the tail of every container log and the
// node-health lines of the k3d node containers. Output goes to t.Log and, when
// INTEGRATION_ARTIFACTS_DIR is set, to files under <dir>/<test>/<cluster>/.
type diagnostics struct {
	t         testing.TB
	cluster   string
	host      *k3d.Cluster
	config    *rest.Config
	scheme    *runtime.Scheme
	namespace string
	recorder  *podLogRecorder

	files map[string]*strings.Builder
}

func (e *Env) diagnosticsFor(t testing.TB, namespace string) *diagnostics {
	return &diagnostics{
		t:         t,
		cluster:   e.Name,
		host:      e.host,
		config:    e.config,
		scheme:    e.scheme,
		namespace: namespace,
		recorder:  e.recorderFor(namespace),
		files:     map[string]*strings.Builder{},
	}
}

func (d *diagnostics) dump() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	d.t.Logf("=== DIAGNOSTICS for %s (cluster=%s namespace=%s) ===", d.t.Name(), d.cluster, d.namespace)

	c, err := client.New(d.config, client.Options{Scheme: d.scheme})
	if err != nil {
		d.t.Logf("[diagnostics] error creating client: %v", err)
		return
	}

	pods := d.dumpPods(ctx, c)
	d.dumpObjects(ctx, c)
	d.dumpEvents(ctx, c)
	d.dumpNodes(ctx, c)
	d.dumpContainerLogs(ctx, pods)
	d.dumpNodeContainerLogs()
	d.writeArtifacts()
}

func (d *diagnostics) dumpPods(ctx context.Context, c client.Client) []corev1.Pod {
	var podList corev1.PodList
	if err := c.List(ctx, &podList, client.InNamespace(d.namespace)); err != nil {
		d.t.Logf("[diagnostics] error listing pods: %v", err)
		return nil
	}
	for _, pod := range podList.Items {
		ready := "<none>"
		for _, cond := range pod.Status.Conditions {
			if cond.Type == corev1.PodReady {
				ready = fmt.Sprintf("%s reason=%s since=%s", cond.Status, cond.Reason, cond.LastTransitionTime.UTC().Format(time.RFC3339))
			}
		}
		d.line("pod", "%s phase=%s reason=%s node=%s ready=%s", pod.Name, pod.Status.Phase, pod.Status.Reason, pod.Spec.NodeName, ready)
		for _, cs := range pod.Status.ContainerStatuses {
			d.line("pod", "%s/%s ready=%t restarts=%d", pod.Name, cs.Name, cs.Ready, cs.RestartCount)
			if cs.State.Waiting != nil {
				d.line("pod", "%s/%s waiting: %s - %s", pod.Name, cs.Name, cs.State.Waiting.Reason, cs.State.Waiting.Message)
			}
			if cs.State.Terminated != nil {
				d.line("pod", "%s/%s terminated: exitCode=%d reason=%s", pod.Name, cs.Name, cs.State.Terminated.ExitCode, cs.State.Terminated.Reason)
			}
			if last := cs.LastTerminationState.Terminated; last != nil {
				d.line("pod", "%s/%s last terminated: exitCode=%d reason=%s at=%s", pod.Name, cs.Name, last.ExitCode, last.Reason, last.FinishedAt.UTC().Format(time.RFC3339))
			}
		}
	}
	return podList.Items
}

// dumpObjects lists the workload objects in the namespace with the fields that
// gate deletion: finalizers, deletion timestamps and StatefulSet replica
// counts. Redpanda CRDs are listed only when the env's scheme knows them.
func (d *diagnostics) dumpObjects(ctx context.Context, c client.Client) {
	var sets appsv1.StatefulSetList
	if err := c.List(ctx, &sets, client.InNamespace(d.namespace)); err != nil {
		d.t.Logf("[diagnostics] error listing statefulsets: %v", err)
	}
	for _, set := range sets.Items {
		d.line("object", "StatefulSet/%s replicas=%d status.replicas=%d ready=%d updateRevision=%s deletionTimestamp=%s finalizers=%v",
			set.Name, ptr.Deref(set.Spec.Replicas, 1), set.Status.Replicas, set.Status.ReadyReplicas, set.Status.UpdateRevision, formatTime(set.DeletionTimestamp), set.Finalizers)
	}

	gv := schema.GroupVersion{Group: redpandav1alpha2.GroupVersion.Group, Version: redpandav1alpha2.GroupVersion.Version}
	for _, kind := range []string{"StretchCluster", "RedpandaBrokerPool", "Redpanda", "NodePool"} {
		if !d.scheme.Recognizes(gv.WithKind(kind)) {
			continue
		}
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(gv.WithKind(kind + "List"))
		if err := c.List(ctx, list, client.InNamespace(d.namespace)); err != nil {
			d.t.Logf("[diagnostics] error listing %s: %v", kind, err)
			continue
		}
		for _, obj := range list.Items {
			d.line("object", "%s/%s deletionTimestamp=%s finalizers=%v", kind, obj.GetName(), formatTime(obj.GetDeletionTimestamp()), obj.GetFinalizers())
		}
	}
}

func formatTime(t *metav1.Time) string {
	if t == nil {
		return "<none>"
	}
	return t.UTC().Format(time.RFC3339)
}

func (d *diagnostics) dumpEvents(ctx context.Context, c client.Client) {
	var eventList corev1.EventList
	if err := c.List(ctx, &eventList, client.InNamespace(d.namespace)); err != nil {
		d.t.Logf("[diagnostics] error listing events: %v", err)
		return
	}
	d.lineEvents("event", eventList.Items)
}

// dumpNodes records every node's Ready condition and taints plus the Node
// events, which carry the NodeNotReady/NodeReady transitions and their times.
func (d *diagnostics) dumpNodes(ctx context.Context, c client.Client) {
	var nodeList corev1.NodeList
	if err := c.List(ctx, &nodeList); err != nil {
		d.t.Logf("[diagnostics] error listing nodes: %v", err)
	}
	for _, node := range nodeList.Items {
		ready := "<none>"
		for _, cond := range node.Status.Conditions {
			if cond.Type == corev1.NodeReady {
				ready = fmt.Sprintf("%s reason=%s since=%s", cond.Status, cond.Reason, cond.LastTransitionTime.UTC().Format(time.RFC3339))
			}
		}
		taints := make([]string, 0, len(node.Spec.Taints))
		for _, taint := range node.Spec.Taints {
			taints = append(taints, taint.ToString())
		}
		d.line("node", "%s ready=%s taints=%v", node.Name, ready, taints)
	}

	var eventList corev1.EventList
	if err := c.List(ctx, &eventList, client.MatchingFieldsSelector{Selector: fields.OneTermEqualSelector("involvedObject.kind", "Node")}); err != nil {
		d.t.Logf("[diagnostics] error listing node events: %v", err)
		return
	}
	d.lineEvents("node-event", eventList.Items)
}

// dumpContainerLogs prints the tail of every container log the recorder
// followed — including pods that no longer exist, which is where a broker's
// shutdown is logged — and fetches live tails for anything it never saw. The
// sidecar's log is where the readiness probe explains why a broker is unready.
func (d *diagnostics) dumpContainerLogs(ctx context.Context, pods []corev1.Pod) {
	recorded := map[string][]string{}
	if d.recorder != nil {
		var errs []string
		recorded, errs = d.recorder.snapshot()
		for _, e := range errs {
			d.t.Logf("[diagnostics] log recorder: %s", e)
		}
		keys := make([]string, 0, len(recorded))
		for key := range recorded {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			d.logBlock(key, recorded[key])
		}
	}

	if len(pods) == 0 {
		return
	}
	ctl, err := kube.FromRESTConfig(d.config)
	if err != nil {
		d.t.Logf("[diagnostics] error creating kube client for logs: %v", err)
		return
	}
	for i := range pods {
		pod := &pods[i]
		for _, cs := range pod.Status.ContainerStatuses {
			if _, ok := recorded[containerLogKey(pod.Name, cs.Name, cs.RestartCount)]; ok {
				continue
			}
			d.containerLog(ctx, ctl, pod, cs.Name, false)
			if cs.RestartCount > 0 {
				d.containerLog(ctx, ctl, pod, cs.Name, true)
			}
		}
	}
}

func (d *diagnostics) containerLog(ctx context.Context, ctl *kube.Ctl, pod *corev1.Pod, container string, previous bool) {
	name := pod.Name + "/" + container
	if previous {
		name += ".previous"
	}
	stream, err := ctl.Logs(ctx, pod, corev1.PodLogOptions{
		Container: container,
		Previous:  previous,
		TailLines: ptr.To(int64(diagnosticsLogTailLines)),
	})
	if err != nil {
		d.t.Logf("[diagnostics] error fetching logs of %s: %v", name, err)
		return
	}
	defer stream.Close()
	logs, err := io.ReadAll(stream)
	if err != nil {
		d.t.Logf("[diagnostics] error reading logs of %s: %v", name, err)
		return
	}
	d.logBlock(name, strings.Split(strings.TrimRight(string(logs), "\n"), "\n"))
}

func (d *diagnostics) logBlock(name string, lines []string) {
	d.t.Logf("[diagnostics/log] %s (last %d lines):\n%s", name, len(lines), strings.Join(lines, "\n"))
	file := strings.NewReplacer("/", "-", "#", ".").Replace(name)
	d.file(filepath.Join("logs", file+".log"), strings.Join(lines, "\n")+"\n")
}

// dumpNodeContainerLogs pulls the node-health lines out of each k3d node
// container's k3s log; kubelet and the node controller both log there.
func (d *diagnostics) dumpNodeContainerLogs() {
	if d.host == nil {
		return
	}
	out, err := exec.Command("docker", "ps", "--filter", "name=^k3d-"+d.host.Name+"-", "--format", "{{.Names}}").Output()
	if err != nil {
		d.t.Logf("[diagnostics] error listing k3d node containers: %v", err)
		return
	}
	for _, container := range strings.Fields(string(out)) {
		if strings.Contains(container, "-serverlb") {
			continue
		}
		logs, err := exec.Command("docker", "logs", "--tail", "2000", container).CombinedOutput()
		if err != nil {
			d.t.Logf("[diagnostics] error reading logs of %s: %v", container, err)
			continue
		}
		var matched []string
		for _, line := range strings.Split(string(logs), "\n") {
			if nodeHealthLogPattern.MatchString(line) {
				matched = append(matched, line)
			}
		}
		if len(matched) > diagnosticsLogTailLines {
			matched = matched[len(matched)-diagnosticsLogTailLines:]
		}
		d.t.Logf("[diagnostics/k3d] %s node-health log lines (%d):\n%s", container, len(matched), strings.Join(matched, "\n"))
		d.file(filepath.Join("k3d", container+".log"), string(logs))
	}
}

func (d *diagnostics) lineEvents(kind string, events []corev1.Event) {
	sort.Slice(events, func(i, j int) bool {
		return eventTime(events[i]).Before(eventTime(events[j]))
	})
	for _, event := range events {
		d.line(kind, "%s %s %s/%s: %s (reason=%s count=%d)", eventTime(event).UTC().Format(time.RFC3339), event.Type, event.InvolvedObject.Kind, event.InvolvedObject.Name, event.Message, event.Reason, event.Count)
	}
}

// eventTime is the most recent occurrence of an event, whichever of the
// legacy and events.k8s.io fields the reporter populated.
func eventTime(event corev1.Event) time.Time {
	switch {
	case !event.LastTimestamp.IsZero():
		return event.LastTimestamp.Time
	case !event.EventTime.IsZero():
		return event.EventTime.Time
	default:
		return event.CreationTimestamp.Time
	}
}

func (d *diagnostics) line(kind, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	d.t.Logf("[diagnostics/%s] %s", kind, msg)
	d.file(kind+".txt", msg+"\n")
}

func (d *diagnostics) file(name, content string) {
	b, ok := d.files[name]
	if !ok {
		b = &strings.Builder{}
		d.files[name] = b
	}
	b.WriteString(content)
}

func (d *diagnostics) writeArtifacts() {
	artifactsDir := os.Getenv("INTEGRATION_ARTIFACTS_DIR")
	if artifactsDir == "" {
		return
	}
	dir := filepath.Join(artifactsDir, sanitizeTestName(d.t.Name()), d.cluster)
	for name, content := range d.files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			d.t.Logf("[diagnostics] failed to create artifacts directory %s: %v", filepath.Dir(path), err)
			return
		}
		if err := os.WriteFile(path, []byte(content.String()), 0o600); err != nil {
			d.t.Logf("[diagnostics] failed to write %s: %v", path, err)
		}
	}
}

// recordLogs follows the container logs of every pod in namespace until t
// ends, so a later dump can show what pods logged before they were deleted.
func (e *Env) recordLogs(t testing.TB, namespace string) {
	recorder, err := startPodLogRecorder(e.config, e.scheme, namespace)
	if err != nil {
		t.Logf("[diagnostics] not recording pod logs for %s on %s: %v", namespace, e.Name, err)
		return
	}
	e.recordersMu.Lock()
	if e.recorders == nil {
		e.recorders = map[string]*podLogRecorder{}
	}
	e.recorders[namespace] = recorder
	e.recordersMu.Unlock()

	// Registered before the dump cleanup so (LIFO) it runs after the dump.
	t.Cleanup(func() {
		recorder.stop()
		e.recordersMu.Lock()
		delete(e.recorders, namespace)
		e.recordersMu.Unlock()
	})
}

func (e *Env) recorderFor(namespace string) *podLogRecorder {
	e.recordersMu.Lock()
	defer e.recordersMu.Unlock()
	return e.recorders[namespace]
}

// podLogRecorder follows every container log in a namespace for the life of a
// test, keeping the last diagnosticsLogTailLines lines per container instance,
// so the diagnostics dump can show what a pod logged before it was deleted.
// It never logs through testing.TB: its goroutines outlive individual tests.
type podLogRecorder struct {
	ctx    context.Context
	cancel context.CancelFunc
	client client.WithWatch
	ctl    *kube.Ctl
	ns     string

	mu        sync.Mutex
	following map[string]bool
	logs      map[string][]string
	errs      []string
}

func startPodLogRecorder(config *rest.Config, scheme *runtime.Scheme, namespace string) (*podLogRecorder, error) {
	c, err := client.NewWithWatch(config, client.Options{Scheme: scheme})
	if err != nil {
		return nil, err
	}
	ctl, err := kube.FromRESTConfig(config)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &podLogRecorder{
		ctx:       ctx,
		cancel:    cancel,
		client:    c,
		ctl:       ctl,
		ns:        namespace,
		following: map[string]bool{},
		logs:      map[string][]string{},
	}
	go r.run()
	return r, nil
}

func (r *podLogRecorder) stop() {
	r.cancel()
}

// run keeps a pod watch open on the namespace and starts following each
// container instance the first time it is seen running or terminated.
func (r *podLogRecorder) run() {
	for r.ctx.Err() == nil {
		w, err := r.client.Watch(r.ctx, &corev1.PodList{}, client.InNamespace(r.ns))
		if err != nil {
			r.noteErr(fmt.Sprintf("watching pods: %v", err))
		} else {
			for event := range w.ResultChan() {
				if pod, ok := event.Object.(*corev1.Pod); ok {
					r.followNewContainers(pod)
				}
			}
			w.Stop()
		}
		select {
		case <-r.ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

func (r *podLogRecorder) followNewContainers(pod *corev1.Pod) {
	statuses := append(append([]corev1.ContainerStatus{}, pod.Status.InitContainerStatuses...), pod.Status.ContainerStatuses...)
	for _, cs := range statuses {
		if cs.State.Running == nil && cs.State.Terminated == nil {
			continue
		}
		key := containerLogKey(pod.Name, cs.Name, cs.RestartCount)
		r.mu.Lock()
		seen := r.following[key]
		r.following[key] = true
		r.mu.Unlock()
		if !seen {
			go r.follow(pod.DeepCopy(), cs.Name, key)
		}
	}
}

// follow streams one container instance's log until it ends (the container
// exited, the pod is gone or the recorder stopped).
func (r *podLogRecorder) follow(pod *corev1.Pod, container, key string) {
	stream, err := r.ctl.Logs(r.ctx, pod, corev1.PodLogOptions{Container: container, Follow: true})
	if err != nil {
		if r.ctx.Err() == nil {
			r.noteErr(fmt.Sprintf("following %s: %v", key, err))
		}
		r.mu.Lock()
		delete(r.following, key)
		r.mu.Unlock()
		return
	}
	defer stream.Close()
	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		r.mu.Lock()
		lines := append(r.logs[key], scanner.Text())
		if len(lines) > diagnosticsLogTailLines {
			lines = lines[len(lines)-diagnosticsLogTailLines:]
		}
		r.logs[key] = lines
		r.mu.Unlock()
	}
}

func (r *podLogRecorder) noteErr(msg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.errs) < 20 {
		r.errs = append(r.errs, msg)
	}
}

// snapshot returns copies of the recorded log tails keyed by
// pod/container#restartCount, and the recorder's own errors.
func (r *podLogRecorder) snapshot() (map[string][]string, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	logs := make(map[string][]string, len(r.logs))
	for key, lines := range r.logs {
		logs[key] = append([]string(nil), lines...)
	}
	return logs, append([]string(nil), r.errs...)
}

func containerLogKey(pod, container string, restartCount int32) string {
	return fmt.Sprintf("%s/%s#%d", pod, container, restartCount)
}

func sanitizeTestName(name string) string {
	// Replace path separators and non-filesystem-safe characters.
	replacer := strings.NewReplacer("/", "_", "\\", "_", " ", "_")
	return replacer.Replace(name)
}

func RandString(length int) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

	name := ""
	for i := 0; i < length; i++ {
		//nolint:gosec // not meant to be a secure random string.
		name += string(alphabet[rand.IntN(len(alphabet))])
	}

	return name
}

func dupCRDs(crds []*apiextensionsv1.CustomResourceDefinition) []*apiextensionsv1.CustomResourceDefinition {
	cloned := []*apiextensionsv1.CustomResourceDefinition{}
	for _, crd := range crds {
		cloned = append(cloned, crd.DeepCopy())
	}
	return cloned
}
