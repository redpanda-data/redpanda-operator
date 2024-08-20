package client

import (
	"context"
	"errors"

	"github.com/go-logr/logr"
	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/redpanda-data/helm-charts/pkg/redpanda"
	redpandav1alpha1 "github.com/redpanda-data/redpanda-operator/src/go/k8s/api/redpanda/v1alpha1"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/src/go/k8s/api/redpanda/v1alpha2"
	"github.com/twmb/franz-go/pkg/kgo"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	ErrEmptyBrokerList             = errors.New("empty broker list")
	ErrEmptyURLList                = errors.New("empty url list")
	ErrInvalidKafkaClientObject    = errors.New("cannot initialize Kafka API client from given object")
	ErrInvalidRedpandaClientObject = errors.New("cannot initialize Redpanda Admin API client from given object")
	ErrUnsupportedSASLMechanism    = errors.New("unsupported SASL mechanism")
)

// ClientFactory is responsible for creating both high-level and low-level clients used in our
// controllers.
//
// Calling its `Kafka*` methods will initialize a low-level kgo.Client instance
// based on the connection parameters contained within the corresponding CRD struct passed in
// at method invocation.
//
// Calling its `RedpandaAdmin*` methods will initialize a low-level rpadmin.AdminAPI instance
// based on the connection parameters contained within the corresponding CRD struct passed in
// at method invocation.
//
// Additionally, high-level clients that act as functional wrappers to the low-level
// clients to ease high-level operations (i.e. syncing all primitives for a v1alpha2.User) may
// be initialized through calling the appropriate `*Client` methods.
type ClientFactory interface {
	// WithDialer causes the underlying connections to use a different dialer implementation.
	WithDialer(dialer redpanda.DialContextFunc) ClientFactory
	// WithLogger specifies a logger to be passed to the underlying clients.
	WithLogger(logger logr.Logger) ClientFactory

	// KafkaClient initializes a kgo.Client based on the spec of the passed in struct.
	// The struct *must* implement either the v1alpha2.KafkaConnectedObject interface of the v1alpha2.ClusterReferencingObject
	// interface to properly initialize.
	KafkaClient(ctx context.Context, object client.Object, opts ...kgo.Opt) (*kgo.Client, error)

	// RedpandaAdminClient initializes a rpadmin.AdminAPI client based on the spec of the passed in struct.
	// The struct *must* implement either the v1alpha2.AdminConnectedObject interface of the v1alpha2.ClusterReferencingObject
	// interface to properly initialize.
	RedpandaAdminClient(ctx context.Context, object client.Object) (*rpadmin.AdminAPI, error)
}

type clientFactory struct {
	client.Client
	logger logr.Logger
	config *rest.Config
	dialer redpanda.DialContextFunc
}

var _ ClientFactory = (*clientFactory)(nil)

func NewClientFactory(config *rest.Config) (ClientFactory, error) {
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		return nil, err
	}

	if err := redpandav1alpha2.AddToScheme(s); err != nil {
		return nil, err
	}

	if err := redpandav1alpha1.AddToScheme(s); err != nil {
		return nil, err
	}

	c, err := client.New(config, client.Options{Scheme: s})
	if err != nil {
		return nil, err
	}

	return &clientFactory{
		Client: c,
		config: config,
		logger: logr.Discard(),
	}, nil
}

func (c *clientFactory) WithDialer(dialer redpanda.DialContextFunc) ClientFactory {
	c.dialer = dialer
	return c
}

func (c *clientFactory) WithLogger(logger logr.Logger) ClientFactory {
	c.logger = logger
	return c
}

func (c *clientFactory) KafkaClient(ctx context.Context, obj client.Object, opts ...kgo.Opt) (*kgo.Client, error) {
	// if we pass in a Redpanda cluster, just use it
	if cluster, ok := obj.(*redpandav1alpha2.Redpanda); ok {
		return c.kafkaForCluster(ctx, cluster, opts...)
	}

	cluster, err := c.getCluster(ctx, obj)
	if err != nil {
		return nil, err
	}

	if cluster != nil {
		return c.kafkaForCluster(ctx, cluster, opts...)
	}

	if spec := c.getKafkaSpec(obj); spec != nil {
		return c.kafkaForSpec(ctx, obj.GetNamespace(), c.getKafkaMetricNamespace(obj), spec, opts...)
	}

	return nil, ErrInvalidKafkaClientObject
}

func (c *clientFactory) RedpandaAdminClient(ctx context.Context, obj client.Object) (*rpadmin.AdminAPI, error) {
	// if we pass in a Redpanda cluster, just use it
	if cluster, ok := obj.(*redpandav1alpha2.Redpanda); ok {
		return c.redpandaAdminForCluster(ctx, cluster)
	}

	cluster, err := c.getCluster(ctx, obj)
	if err != nil {
		return nil, err
	}

	if cluster != nil {
		return c.redpandaAdminForCluster(ctx, cluster)
	}

	if spec := c.getAdminSpec(obj); spec != nil {
		return c.redpandaAdminForSpec(ctx, obj.GetNamespace(), spec)
	}

	return nil, ErrInvalidRedpandaClientObject
}

func (c *clientFactory) getCluster(ctx context.Context, obj client.Object) (*redpandav1alpha2.Redpanda, error) {
	if o, ok := obj.(redpandav1alpha2.ClusterReferencingObject); ok {
		if ref := o.GetClusterRef(); ref != nil {
			var cluster redpandav1alpha2.Redpanda

			if err := c.Get(ctx, types.NamespacedName{Namespace: obj.GetNamespace(), Name: ref.Name}, &cluster); err != nil {
				return nil, err
			}

			return &cluster, nil
		}
	}

	return nil, nil
}

func (c *clientFactory) getKafkaSpec(obj client.Object) *redpandav1alpha2.KafkaAPISpec {
	if o, ok := obj.(redpandav1alpha2.KafkaConnectedObject); ok {
		return o.GetKafkaAPISpec()
	}
	return nil
}

func (c *clientFactory) getKafkaMetricNamespace(obj client.Object) *string {
	if o, ok := obj.(redpandav1alpha2.KafkaConnectedObjectWithMetrics); ok {
		return o.GetMetricsNamespace()
	}
	return nil
}

func (c *clientFactory) getAdminSpec(obj client.Object) *redpandav1alpha2.AdminAPISpec {
	if o, ok := obj.(redpandav1alpha2.AdminConnectedObject); ok {
		return o.GetAdminAPISpec()
	}
	return nil
}
