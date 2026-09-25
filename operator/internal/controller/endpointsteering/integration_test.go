// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package endpointsteering_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redpanda-data/common-go/kube"
	"github.com/redpanda-data/common-go/kube/kubetest"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	crds "github.com/redpanda-data/redpanda-operator/operator/config/crd/bases"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller/endpointsteering"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/labels"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
)

// kafkaTestPort is the Kafka port the steered Services under test carry; it
// exists only to be a port the Schema Registry probe has no say over.
const kafkaTestPort = 9092

// TestSteersSchemaRegistryPort drives the controller against a real API
// server, for both cluster kinds at once as the operator runs them: broker
// pods, a steered Service each, and one stand-in Schema Registry the test can
// stop answering. It covers what no unit test can -- that the decisions reach
// the published EndpointSlices, per port, through the same factory and
// resolvers the operator resolves clusters with -- and it covers the
// transitions in both directions, which the acceptance scenarios cannot,
// since nothing can put a real broker's registry back into replay on demand.
func TestSteersSchemaRegistryPort(t *testing.T) {
	// The published addresses have to be real pod addresses as far as the API
	// server is concerned -- it rejects loopback in an EndpointSlice -- and
	// the probes then dial exactly those, so the stand-in registry has to
	// listen on an address this host actually answers on.
	listener, err := net.Listen("tcp", net.JoinHostPort(routableHostAddress(t), "0"))
	require.NoError(t, err)

	var serving atomic.Bool
	serving.Store(true)
	registry := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !serving.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	registry.Listener = listener
	registry.Start()
	t.Cleanup(registry.Close)
	address := registry.Listener.Addr().(*net.TCPAddr)

	ctl := kubetest.NewEnv(t, kube.Options{Options: client.Options{Scheme: controller.UnifiedScheme}})
	require.NoError(t, kube.ApplyAllAndWait(t.Context(), ctl, func(crd *apiextensionsv1.CustomResourceDefinition, err error) (bool, error) {
		if err != nil {
			return false, err
		}
		for _, condition := range crd.Status.Conditions {
			if condition.Type == apiextensionsv1.Established {
				return condition.Status == apiextensionsv1.ConditionTrue, nil
			}
		}
		return false, nil
	}, crds.All()...))

	namespace, err := kube.Create(t.Context(), ctl, corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "steering-"},
	})
	require.NoError(t, err)

	clusters := []steeredCluster{
		applyV1Cluster(t, ctl, namespace.Name, "rp-v1", address),
		applyV2Cluster(t, ctl, namespace.Name, "rp-v2", address),
	}

	kubetest.RunManager(t, ctl, kubetest.WithRegisterFn(func(mgr ctrl.Manager) error {
		// A V2 cluster's shape comes from a chart render, which needs the
		// cluster's own REST config, so the factory needs a manager; nothing
		// reads through its cache here, so it is never started.
		manager, err := multicluster.NewSingleClusterManager(ctl.RestConfig(), ctrl.Options{
			Scheme:                 controller.UnifiedScheme,
			Metrics:                metricsserver.Options{BindAddress: "0"},
			HealthProbeBindAddress: "0",
		})
		if err != nil {
			return err
		}
		factory := internalclient.NewFactory(manager, nil)

		// Both resolvers, in the order the run command registers them, so a
		// Service naming either kind of cluster is served.
		// Resync fast: the assertions below wait out the damping thresholds.
		return endpointsteering.Setup(mgr, endpointsteering.Options{
			Resolver: endpointsteering.Resolvers{
				endpointsteering.V2Resolver(mgr.GetClient(), endpointsteering.BrokersOf(factory.ClusterBrokers)),
				endpointsteering.V1Resolver(mgr.GetClient(), endpointsteering.BrokersOf(factory.ClusterBrokers)),
			},
			ResyncPeriod:  250 * time.Millisecond,
			ClusterDomain: "cluster.local",
		})
	}))

	requirePublished := func(port string, want func(steeredCluster) []string) {
		t.Helper()
		for _, cluster := range clusters {
			var last []string
			require.Eventuallyf(t, func() bool {
				published, err := publishedPods(t.Context(), ctl, namespace.Name, cluster.service, port)
				if err != nil {
					return false
				}
				last = published
				return slices.Equal(published, want(cluster))
			}, 30*time.Second, 100*time.Millisecond, "%s: port %q published %v, wanted %v", cluster.name, port, last, want(cluster))
		}
	}
	allBrokers := func(cluster steeredCluster) []string { return cluster.pods }
	none := func(steeredCluster) []string { return nil }

	requirePublished("kafka", allBrokers)
	requirePublished("schema-registry", allBrokers)

	// A registry that stops answering leaves its own port, and nothing else.
	serving.Store(false)
	requirePublished("schema-registry", none)
	requirePublished("kafka", allBrokers)

	serving.Store(true)
	requirePublished("schema-registry", allBrokers)
}

// steeredCluster is one cluster under test: the Service the operator steers
// for it, and the broker pods it should publish.
type steeredCluster struct {
	name    string
	service string
	pods    []string
}

// applyV1Cluster creates a v1 Cluster, its broker pods, and a Service steered
// for it, with its Schema Registry listener pointed at address.
func applyV1Cluster(t *testing.T, ctl *kube.Ctl, namespace, name string, address *net.TCPAddr) steeredCluster {
	t.Helper()

	_, err := kube.Create(t.Context(), ctl, vectorizedv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: vectorizedv1alpha1.ClusterSpec{
			Image:    "redpandadata/redpanda",
			Version:  "dev",
			Replicas: ptr.To(int32(3)),
			Configuration: vectorizedv1alpha1.RedpandaConfig{
				RPCServer:      vectorizedv1alpha1.SocketAddress{Port: 33145},
				KafkaAPI:       []vectorizedv1alpha1.KafkaAPI{{Port: kafkaTestPort}},
				AdminAPI:       []vectorizedv1alpha1.AdminAPI{{Port: 9644}},
				SchemaRegistry: &vectorizedv1alpha1.SchemaRegistryAPI{Port: address.Port},
			},
		},
	})
	require.NoError(t, err)

	// The labels the v1 operator selects its brokers with.
	podLabels := labels.ForCluster(&vectorizedv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}).AsAPISelector().MatchLabels

	return steeredCluster{
		name:    "v1 Cluster",
		service: applySteeredService(t, ctl, namespace, name, address),
		pods:    applyBrokerPods(t, ctl, namespace, name, podLabels, address),
	}
}

// applyV2Cluster is applyV1Cluster for a v2 Redpanda.
func applyV2Cluster(t *testing.T, ctl *kube.Ctl, namespace, name string, address *net.TCPAddr) steeredCluster {
	t.Helper()

	_, err := kube.Create(t.Context(), ctl, redpandav1alpha2.Redpanda{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: redpandav1alpha2.RedpandaSpec{
			ClusterSpec: &redpandav1alpha2.RedpandaClusterSpec{
				// TLS off, so the listener the probes dial is the plaintext
				// stand-in.
				TLS: &redpandav1alpha2.TLS{Enabled: ptr.To(false)},
				Listeners: &redpandav1alpha2.Listeners{
					Kafka:          &redpandav1alpha2.Kafka{Listener: redpandav1alpha2.Listener{Port: ptr.To(int32(kafkaTestPort))}},
					SchemaRegistry: &redpandav1alpha2.SchemaRegistry{Listener: redpandav1alpha2.Listener{Port: ptr.To(int32(address.Port))}},
				},
			},
		},
	})
	require.NoError(t, err)

	return steeredCluster{
		name:    "v2 Redpanda",
		service: applySteeredService(t, ctl, namespace, name, address),
		pods: applyBrokerPods(t, ctl, namespace, name, map[string]string{
			"app.kubernetes.io/instance": name,
			"app.kubernetes.io/name":     "redpanda",
		}, address),
	}
}

// applySteeredService creates a Service shaped as the operator renders the
// clusters' own: annotated for the cluster, no selector, and publishing
// not-ready addresses because Raft needs them.
func applySteeredService(t *testing.T, ctl *kube.Ctl, namespace, cluster string, address *net.TCPAddr) string {
	t.Helper()

	name := cluster + "-steered"
	_, err := kube.Create(t.Context(), ctl, corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: map[string]string{endpointsteering.ServiceAnnotation: cluster},
		},
		Spec: corev1.ServiceSpec{
			PublishNotReadyAddresses: true,
			Ports: []corev1.ServicePort{
				{Name: "kafka", Port: kafkaTestPort, TargetPort: intstr.FromInt32(kafkaTestPort), Protocol: corev1.ProtocolTCP},
				{Name: "schema-registry", Port: int32(address.Port), TargetPort: intstr.FromInt32(int32(address.Port)), Protocol: corev1.ProtocolTCP},
			},
		},
	})
	require.NoError(t, err)
	return name
}

// applyBrokerPods creates three pods that a cluster's Services will take as
// brokers, every one of them answering at address.
func applyBrokerPods(t *testing.T, ctl *kube.Ctl, namespace, cluster string, podLabels map[string]string, address *net.TCPAddr) []string {
	t.Helper()

	var names []string
	for i := range 3 {
		name := fmt.Sprintf("%s-%d", cluster, i)
		names = append(names, name)

		pod, err := kube.Create(t.Context(), ctl, corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: podLabels},
			Spec: corev1.PodSpec{
				Hostname: name,
				// The subdomain a broker pod names, which is what gives it
				// the DNS record a TLS listener's probe verifies against.
				Subdomain: cluster,
				// The API server requires a container; nothing runs it.
				Containers: []corev1.Container{{Name: "redpanda", Image: "redpanda"}},
			},
		})
		require.NoError(t, err)

		// The registry stand-in listens on one address, so that is where
		// every pod's probes have to be pointed.
		pod.Status = corev1.PodStatus{
			Phase:      corev1.PodRunning,
			PodIP:      address.IP.String(),
			PodIPs:     []corev1.PodIP{{IP: address.IP.String()}},
			Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
		}
		require.NoError(t, ctl.UpdateStatus(t.Context(), pod))
	}
	return names
}

// publishedPods returns, sorted, the pods the operator publishes for one
// port of a Service.
func publishedPods(ctx context.Context, ctl *kube.Ctl, namespace, service, port string) ([]string, error) {
	var endpointSlices discoveryv1.EndpointSliceList
	if err := ctl.List(ctx, namespace, &endpointSlices, client.MatchingLabels{
		discoveryv1.LabelServiceName: service,
		discoveryv1.LabelManagedBy:   endpointsteering.ManagedBy,
	}); err != nil {
		return nil, err
	}

	names := []string{}
	for i := range endpointSlices.Items {
		slice := &endpointSlices.Items[i]
		if !slices.ContainsFunc(slice.Ports, func(p discoveryv1.EndpointPort) bool { return ptr.Deref(p.Name, "") == port }) {
			continue
		}
		for _, endpoint := range slice.Endpoints {
			if endpoint.TargetRef != nil {
				names = append(names, endpoint.TargetRef.Name)
			}
		}
	}
	slices.Sort(names)
	return names, nil
}

// routableHostAddress is a non-loopback IPv4 address of this host, which is
// what an EndpointSlice will accept as an endpoint and what a probe can then
// reach.
func routableHostAddress(t *testing.T) string {
	t.Helper()

	addresses, err := net.InterfaceAddrs()
	require.NoError(t, err)
	for _, address := range addresses {
		ipNet, ok := address.(*net.IPNet)
		if !ok || ipNet.IP.IsLoopback() || ipNet.IP.To4() == nil {
			continue
		}
		return ipNet.IP.String()
	}
	t.Skip("no non-loopback IPv4 address on this host to stand in for a pod address")
	return ""
}
