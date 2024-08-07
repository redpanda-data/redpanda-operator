package testutils

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	helmclient "github.com/mittwald/go-helm-client"
	"github.com/mittwald/go-helm-client/values"
	clusterredpandacomv1alpha1 "github.com/redpanda-data/redpanda-operator/src/go/k8s/api/cluster.redpanda.com/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/src/go/k8s/api/redpanda/v1alpha2"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
	"gopkg.in/yaml.v2"
	"helm.sh/helm/v3/pkg/repo"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	serviceLabel = "kubernetes.io/service-name"

	defaultTimeout          = 1 * time.Minute
	defaultK3sImage         = "rancher/k3s:v1.27.1-k3s1"
	defaultHelmChart        = "redpandadata/redpanda"
	defaultHelmRepo         = "redpandadata"
	defaultHelmURL          = "https://charts.redpanda.com"
	defaultCertManagerChart = "jetstack/cert-manager"
	defaultCertManagerRepo  = "jetstack"
	defaultCertManagerURL   = "https://charts.jetstack.io"
)

var defaultPorts = map[string]int{
	"admin":          9644,
	"http":           8082,
	"kafka":          9093,
	"rpc":            33145,
	"schemaRegistry": 8081,
}

type K3sStackOptions struct {
	K3sImage            string
	CertManagerURL      string
	CertManagerRepo     string
	CertManagerChart    string
	CertManagerVersion  string
	Driver              string
	InstallationTimeout time.Duration
}

func (k K3sStackOptions) normalize() K3sStackOptions {
	if k.K3sImage == "" {
		k.K3sImage = defaultK3sImage
	}

	if k.CertManagerURL == "" {
		k.CertManagerURL = defaultCertManagerURL
	}

	if k.CertManagerRepo == "" {
		k.CertManagerRepo = defaultCertManagerRepo
	}

	if k.CertManagerChart == "" {
		k.CertManagerChart = defaultCertManagerChart
	}

	if k.InstallationTimeout == 0 {
		k.InstallationTimeout = defaultTimeout
	}

	return k
}

type RedpandaOptions struct {
	Name        string
	Namespace   string
	HelmURL     string
	HelmRepo    string
	HelmChart   string
	HelmVersion string
	Values      map[string]interface{}
}

func (r RedpandaOptions) normalize(suffix int32) RedpandaOptions {
	defaultNameAndNamespace := fmt.Sprintf("k3s-%d-%d", time.Now().Unix(), suffix)

	if r.Name == "" {
		r.Name = defaultNameAndNamespace
	}

	if r.Namespace == "" {
		r.Namespace = defaultNameAndNamespace
	}

	if r.HelmURL == "" {
		r.HelmURL = defaultHelmURL
	}

	if r.HelmRepo == "" {
		r.HelmRepo = defaultHelmRepo
	}

	if r.HelmChart == "" {
		r.HelmChart = defaultHelmChart
	}

	if r.Values == nil {
		r.Values = map[string]interface{}{}
	}

	return r
}

type K3sStack struct {
	Options       K3sStackOptions
	config        []byte
	helm          helmclient.Client
	directory     string
	client        client.Client
	rest          *rest.Config
	installations atomic.Int32
	clusters      map[int32]*v1alpha2.Redpanda
	portForwarder *portForwarder
	mutex         sync.RWMutex

	ports     []*forwardedPorts
	portMutex sync.RWMutex
}

func RunK3sStack(t *testing.T, options K3sStackOptions) *K3sStack {
	stack := &K3sStack{Options: options.normalize(), clusters: make(map[int32]*v1alpha2.Redpanda)}
	stack.initializeK3s(t)
	stack.initializeHelm(t)
	stack.installCertManagerChart(t)

	stack.portForwarder = newPortForwarder(stack.rest, stack.client)
	return stack
}

func (k *K3sStack) InstallCluster(t *testing.T, options RedpandaOptions) *v1alpha2.Redpanda {
	id := k.installations.Add(1)
	options = options.normalize(id)
	k.installRedpandaChart(t, options)

	jsonData, err := json.Marshal(options.Values)
	require.NoError(t, err)

	cluster := &v1alpha2.Redpanda{
		ObjectMeta: metav1.ObjectMeta{
			Name:      options.Name,
			Namespace: options.Namespace,
		},
		Spec: v1alpha2.RedpandaSpec{
			ClusterSpec: &v1alpha2.RedpandaClusterSpec{},
		},
	}
	err = json.Unmarshal(jsonData, &cluster.Spec.ClusterSpec)
	require.NoError(t, err)

	k.mutex.Lock()
	defer k.mutex.Unlock()

	k.clusters[id] = cluster
	return cluster
}

func (k *K3sStack) Client() client.Client {
	return k.client
}

type portForwarder struct {
	config *rest.Config
	client client.Client
}

func newPortForwarder(config *rest.Config, client client.Client) *portForwarder {
	return &portForwarder{
		config: config,
		client: client,
	}
}

func (p *portForwarder) freePort() (port int, err error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}

	listener, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer listener.Close()

	return listener.Addr().(*net.TCPAddr).Port, nil
}

func getPortFromSpec(spec *v1alpha2.RedpandaClusterSpec, name string) int {
	defaultPort := defaultPorts[name]
	if spec == nil || spec.Listeners == nil {
		return defaultPort
	}
	switch name {
	case "admin":
		if spec.Listeners.Admin != nil && spec.Listeners.Admin.Port != nil {
			return *spec.Listeners.Admin.Port
		}
	case "http":
		if spec.Listeners.HTTP != nil && spec.Listeners.HTTP.Port != nil {
			return *spec.Listeners.HTTP.Port
		}
	case "kafka":
		if spec.Listeners.Kafka != nil && spec.Listeners.Kafka.Port != nil {
			return *spec.Listeners.Kafka.Port
		}
	case "rpc":
		if spec.Listeners.RPC != nil && spec.Listeners.RPC.Port != nil {
			return *spec.Listeners.RPC.Port
		}
	case "schemaRegistry":
		if spec.Listeners.SchemaRegistry != nil && spec.Listeners.SchemaRegistry.Port != nil {
			return *spec.Listeners.SchemaRegistry.Port
		}
	}
	return defaultPort
}

type forwardedPorts struct {
	mappings map[string]string
	cancel   context.CancelFunc
	wg       *sync.WaitGroup
	stopped  atomic.Bool
}

func (p *forwardedPorts) Stop() {
	p.stopped.Store(true)
	p.cancel()
	p.wg.Wait()
}

func (p *forwardedPorts) IsStopped() bool {
	return p.stopped.Load()
}

func (p *portForwarder) forwardClusterPorts(ctx context.Context, cluster *v1alpha2.Redpanda, name string) (*forwardedPorts, error) {
	ctx, cancel := context.WithCancel(ctx)
	targetURL, err := url.Parse(p.config.Host)
	if err != nil {
		cancel()
		return nil, err
	}

	transport, upgrader, err := spdy.RoundTripperFor(p.config)
	if err != nil {
		cancel()
		return nil, err
	}

	port := getPortFromSpec(cluster.Spec.ClusterSpec, name)
	if port == 0 {
		cancel()
		return nil, errors.New("invalid port")
	}

	service := &corev1.Service{}
	if err := p.client.Get(ctx, client.ObjectKeyFromObject(cluster), service); err != nil {
		cancel()
		return nil, err
	}
	endpointSlices := &discoveryv1.EndpointSliceList{}
	if err := p.client.List(ctx, endpointSlices, &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(labels.Set{
			serviceLabel: service.Name,
		}),
		Namespace: service.Namespace,
	}); err != nil {
		cancel()
		return nil, err
	}

	pods := []types.NamespacedName{}
	addresses := []string{}
	hosts := []string{}
	for _, endpointSlice := range endpointSlices.Items {
		for _, endpoint := range endpointSlice.Endpoints {
			if endpoint.TargetRef.Kind != "Pod" {
				continue
			}
			if len(endpoint.Addresses) == 0 {
				continue
			}
			pods = append(pods, types.NamespacedName{
				Namespace: endpoint.TargetRef.Namespace,
				Name:      endpoint.TargetRef.Name,
			})
			host := fmt.Sprintf("%s.%s.%s.svc", endpoint.TargetRef.Name, endpoint.TargetRef.Namespace, service.Name)
			hosts = append(hosts, host)
			addresses = append(addresses, endpoint.Addresses[0])
		}
	}

	var wg sync.WaitGroup
	ports := &forwardedPorts{
		mappings: make(map[string]string),
		cancel:   cancel,
		wg:       &wg,
	}

	for i, pod := range pods {
		address := addresses[i]
		host := hosts[i]
		targetURL.Path = path.Join(
			"api", "v1",
			"namespaces", pod.Namespace,
			"pods", pod.Name,
			"portforward",
		)

		localPort, err := p.freePort()
		if err != nil {
			cancel()
			return nil, err
		}

		dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, targetURL)
		ready := make(chan struct{})
		forwarder, err := portforward.New(dialer, []string{fmt.Sprintf("%d:%d", localPort, port)}, ctx.Done(), ready, io.Discard, io.Discard)
		if err != nil {
			cancel()
			return nil, err
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			forwarder.ForwardPorts()
		}()

		select {
		case <-ctx.Done():
			cancel()
			return nil, ctx.Err()
		case <-ready:
			// map pod by ip
			ports.mappings[fmt.Sprintf("%s:%d", address, port)] = fmt.Sprintf("127.0.0.1:%d", localPort)
			// map pod by .svc dns entry
			ports.mappings[fmt.Sprintf("%s:%d", host, port)] = fmt.Sprintf("127.0.0.1:%d", localPort)
			// map pod by .svc.cluster.local dns entry
			ports.mappings[fmt.Sprintf("%s.cluster.local.:%d", host, port)] = fmt.Sprintf("127.0.0.1:%d", localPort)
		}
	}

	return ports, nil
}

func (k *K3sStack) Forward(t *testing.T, cluster *v1alpha2.Redpanda, name string) {
	ports, err := k.portForwarder.forwardClusterPorts(context.Background(), cluster, name)
	require.NoError(t, err)

	k.portMutex.Lock()
	defer k.portMutex.Unlock()
	k.ports = append(k.ports, ports)

	t.Cleanup(func() {
		ports.Stop()
	})
}

func (k *K3sStack) MapHost(broker string) string {
	k.portMutex.RLock()
	defer k.portMutex.RUnlock()

	for _, forwarded := range k.ports {
		if forwarded.IsStopped() {
			continue
		}
		if mappedHost := forwarded.mappings[broker]; mappedHost != "" {
			return mappedHost
		}
	}

	return broker
}

func (k *K3sStack) ConfigureTLS(brokers []string, config *tls.Config) *tls.Config {
	tlsConfig := config.Clone()
	if tlsConfig.InsecureSkipVerify {
		// no verification needed
		return tlsConfig
	}

	if len(brokers) == 0 {
		return tlsConfig
	}

	broker := brokers[0]
	tokens := strings.SplitN(broker, ":", 2)
	if len(tokens) != 2 {
		return tlsConfig
	}
	serverName := tokens[0]

	// custom connection verification routine to make sure we have
	// the CA we're expecting and that we have a valid server name
	// based on the *original* DNS host prior to remapping the underlying
	// address in our custom dialer.
	tlsConfig.InsecureSkipVerify = true
	tlsConfig.VerifyConnection = func(cs tls.ConnectionState) error {
		opts := x509.VerifyOptions{
			Roots:         config.RootCAs,
			CurrentTime:   time.Now(),
			DNSName:       serverName,
			Intermediates: x509.NewCertPool(),
		}

		for _, cert := range cs.PeerCertificates[1:] {
			opts.Intermediates.AddCert(cert)
		}

		if _, err := cs.PeerCertificates[0].Verify(opts); err != nil {
			return err
		}
		return nil
	}

	return tlsConfig
}

func (k *K3sStack) initializeK3s(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), k.Options.InstallationTimeout)
	defer cancel()

	container, err := k3s.Run(ctx, k.Options.K3sImage)
	require.NoError(t, err)

	t.Cleanup(func() {
		container.Terminate(context.Background())
	})

	config, err := container.GetKubeConfig(ctx)
	require.NoError(t, err)

	k.config = config

	restcfg, err := clientcmd.RESTConfigFromKubeConfig(config)
	require.NoError(t, err)

	k.rest = restcfg

	s := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(s))
	require.NoError(t, v1alpha2.AddToScheme(s))
	require.NoError(t, clusterredpandacomv1alpha1.AddToScheme(s))

	client, err := client.New(restcfg, client.Options{Scheme: s})
	require.NoError(t, err)
	k.client = client
}

func (k *K3sStack) initializeHelm(t *testing.T) {
	dir, err := os.MkdirTemp("", "redpanda-test-helm")
	require.NoError(t, err)
	t.Cleanup(func() {
		os.RemoveAll(dir)
	})
	k.directory = dir

	options := &helmclient.Options{
		RepositoryCache:  path.Join(dir, "cache"),
		RepositoryConfig: path.Join(dir, "repo"),
	}

	kubeconfig := path.Join(dir, "kubeconfig")
	err = os.WriteFile(kubeconfig, k.config, 0644)
	require.NoError(t, err)
	t.Logf("Kubeconfig: %s", kubeconfig)

	client, err := helmclient.NewClientFromKubeConf(&helmclient.KubeConfClientOptions{
		Options:    options,
		KubeConfig: k.config,
	})
	require.NoError(t, err)
	k.helm = client
}

func (k *K3sStack) installCertManagerChart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), k.Options.InstallationTimeout)
	defer cancel()

	chartSpec := helmclient.ChartSpec{
		ReleaseName: "cert-manager",
		ChartName:   k.Options.CertManagerChart,
		Namespace:   "cert-manager",
		ValuesOptions: values.Options{
			Values: []string{"crds.enabled=true"},
		},
		CreateNamespace: true,
		Wait:            true,
		Timeout:         k.Options.InstallationTimeout,
	}

	err := k.helm.AddOrUpdateChartRepo(repo.Entry{
		Name: k.Options.CertManagerRepo,
		URL:  k.Options.CertManagerURL,
	})
	require.NoError(t, err)

	_, err = k.helm.InstallChart(ctx, &chartSpec, nil)
	require.NoError(t, err)
}

func (k *K3sStack) installRedpandaChart(t *testing.T, options RedpandaOptions) {
	ctx, cancel := context.WithTimeout(context.Background(), k.Options.InstallationTimeout)
	defer cancel()

	data, err := yaml.Marshal(options.Values)
	require.NoError(t, err)

	valuesFile := path.Join(k.directory, options.Name+".yaml")
	err = os.WriteFile(valuesFile, data, 0644)
	require.NoError(t, err)

	chartSpec := helmclient.ChartSpec{
		ReleaseName: options.Name,
		ChartName:   options.HelmChart,
		Namespace:   options.Namespace,
		Version:     options.HelmVersion,
		ValuesOptions: values.Options{
			ValueFiles: []string{valuesFile},
		},
		CreateNamespace: true,
		Wait:            true,
		Timeout:         k.Options.InstallationTimeout,
	}

	err = k.helm.AddOrUpdateChartRepo(repo.Entry{
		Name: options.HelmRepo,
		URL:  options.HelmURL,
	})
	require.NoError(t, err)

	_, err = k.helm.InstallChart(ctx, &chartSpec, nil)
	require.NoError(t, err)

	t.Cleanup(func() {
		k.helm.UninstallReleaseByName(chartSpec.ReleaseName)
	})
}

func KafkaAPISpecFromCluster(cluster *v1alpha2.Redpanda) *clusterredpandacomv1alpha1.KafkaAPISpec {
	brokers := []string{}

	replicas := 3
	ss := cluster.Spec.ClusterSpec.Statefulset
	if ss != nil && ss.Replicas != nil {
		replicas = *ss.Replicas
	}

	port := getPortFromSpec(cluster.Spec.ClusterSpec, "kafka")

	for i := 0; i < replicas; i++ {
		broker := fmt.Sprintf("%s-%d.%s.%s.svc:%d", cluster.Name, i, cluster.Name, cluster.Namespace, port)
		brokers = append(brokers, broker)
	}

	spec := &clusterredpandacomv1alpha1.KafkaAPISpec{
		Brokers: brokers,
	}

	tls := cluster.Spec.ClusterSpec.TLS
	if tls == nil || tls.Enabled == nil || *tls.Enabled {
		spec.TLS = &clusterredpandacomv1alpha1.KafkaTLS{
			CaCert: &clusterredpandacomv1alpha1.SecretKeyRef{
				Name: cluster.Name + "-default-cert",
			},
		}
	}

	return spec
}
