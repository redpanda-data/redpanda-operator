// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package client

import (
	"context"
	"errors"

	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/redpanda-data/console/backend/pkg/config"
	"github.com/spf13/afero"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl"
	"github.com/twmb/franz-go/pkg/sasl/scram"
	"github.com/twmb/franz-go/pkg/sr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rpkconfig "github.com/redpanda-data/redpanda/src/go/rpk/pkg/config"

	"github.com/redpanda-data/helm-charts/pkg/redpanda"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/acls"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/schemas"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/users"
)

var (
	ErrInvalidClusterRef                 = errors.New("clusterRef refers to a cluster that does not exist")
	ErrEmptyBrokerList                   = errors.New("empty broker list")
	ErrEmptyURLList                      = errors.New("empty url list")
	ErrInvalidKafkaClientObject          = errors.New("cannot initialize Kafka API client from given object")
	ErrInvalidRedpandaClientObject       = errors.New("cannot initialize Redpanda Admin API client from given object")
	ErrInvalidSchemaRegistryClientObject = errors.New("cannot initialize Schema Registry API client from given object")
	ErrUnsupportedSASLMechanism          = errors.New("unsupported SASL mechanism")
)

// UserAuth allows you to override the auth credentials used in establishing a client connection
type UserAuth struct {
	Username  string
	Password  string
	Mechanism string
}

// ClientFactory is responsible for creating both high-level and low-level clients used in our
// controllers.
//
// Calling its `Kafka*` methods will initialize a low-level [kgo.Client] instance
// based on the connection parameters contained within the corresponding CRD struct passed in
// at method invocation.
//
// Calling its `RedpandaAdmin*` methods will initialize a low-level rpadmin.AdminAPI instance
// based on the connection parameters contained within the corresponding CRD struct passed in
// at method invocation.
type ClientFactory interface {
	// KafkaClient initializes a kgo.Client based on the spec of the passed in struct.
	// The struct *must* either be an RPK profile, Redpanda CR, or implement either the v1alpha2.KafkaConnectedObject interface
	// or the v1alpha2.ClusterReferencingObject interface to properly initialize. Callers should always call Close on the returned *kgo.Client,
	// or it will leak goroutines.
	KafkaClient(ctx context.Context, object any, opts ...kgo.Opt) (*kgo.Client, error)

	// RedpandaAdminClient initializes a rpadmin.AdminAPI client based on the spec of the passed in struct.
	// The struct *must* either be an RPK profile, Redpanda CR, or implement either the v1alpha2.AdminConnectedObject interface
	// or the v1alpha2.ClusterReferencingObject interface to properly initialize.
	RedpandaAdminClient(ctx context.Context, object any) (*rpadmin.AdminAPI, error)

	// SchemaRegistryClient initializes an sr.Client based on the spec of the passed in struct.
	// The struct *must* either be an RPK profile, Redpanda CR, or implement either the v1alpha2.SchemaRegistryConnectedObject interface
	// or the v1alpha2.ClusterReferencingObject interface to properly initialize.
	SchemaRegistryClient(ctx context.Context, object any) (*sr.Client, error)

	// ACLs returns a high-level client for synchronizing ACLs. Callers should always call Close on the returned *acls.Syncer, or it will leak
	// goroutines.
	ACLs(ctx context.Context, object redpandav1alpha2.ClusterReferencingObject, opts ...kgo.Opt) (*acls.Syncer, error)

	// Users returns a high-level client for managing users. Callers should always call Close on the returned *users.Client, or it will leak
	// goroutines.
	Users(ctx context.Context, object redpandav1alpha2.ClusterReferencingObject, opts ...kgo.Opt) (*users.Client, error)

	// Schemas returns a high-level client for synchronizing Schemas.
	Schemas(ctx context.Context, object redpandav1alpha2.ClusterReferencingObject) (*schemas.Syncer, error)
}

type Factory struct {
	client.Client
	config *rest.Config
	fs     afero.Fs

	dialer   redpanda.DialContextFunc
	userAuth *UserAuth
}

var _ ClientFactory = (*Factory)(nil)

func NewFactory(config *rest.Config, kubeclient client.Client) *Factory {
	return &Factory{
		config: rest.CopyConfig(config),
		fs:     afero.NewOsFs(),
		Client: kubeclient,
	}
}

func (c *Factory) WithDialer(dialer redpanda.DialContextFunc) *Factory {
	return &Factory{
		Client:   c.Client,
		config:   c.config,
		userAuth: c.userAuth,
		fs:       c.fs,
		dialer:   dialer,
	}
}

func (c *Factory) WithFS(fs afero.Fs) *Factory {
	return &Factory{
		Client:   c.Client,
		config:   c.config,
		userAuth: c.userAuth,
		dialer:   c.dialer,
		fs:       fs,
	}
}

func (c *Factory) WithUserAuth(userAuth *UserAuth) *Factory {
	return &Factory{
		Client:   c.Client,
		config:   c.config,
		dialer:   c.dialer,
		fs:       c.fs,
		userAuth: userAuth,
	}
}

func (c *Factory) KafkaClient(ctx context.Context, obj any, opts ...kgo.Opt) (*kgo.Client, error) {
	// if we pass in a Redpanda cluster, just use it
	if cluster, ok := obj.(*redpandav1alpha2.Redpanda); ok {
		return c.kafkaForCluster(cluster, opts...)
	}

	if profile, ok := obj.(*rpkconfig.RpkProfile); ok {
		return c.kafkaForRPKProfile(profile, opts...)
	}

	o, ok := obj.(client.Object)
	if !ok {
		return nil, ErrInvalidKafkaClientObject
	}

	cluster, err := c.getCluster(ctx, o)
	if err != nil {
		return nil, err
	}

	if cluster != nil {
		return c.kafkaForCluster(cluster, opts...)
	}

	if spec := c.getKafkaSpec(o); spec != nil {
		return c.kafkaForSpec(ctx, o.GetNamespace(), c.getKafkaMetricNamespace(o), spec, opts...)
	}

	return nil, ErrInvalidKafkaClientObject
}

func (c *Factory) RedpandaAdminClient(ctx context.Context, obj any) (*rpadmin.AdminAPI, error) {
	// if we pass in a Redpanda cluster, just use it
	if cluster, ok := obj.(*redpandav1alpha2.Redpanda); ok {
		return c.redpandaAdminForCluster(cluster)
	}

	if profile, ok := obj.(*rpkconfig.RpkProfile); ok {
		return c.redpandaAdminForRPKProfile(profile)
	}

	o, ok := obj.(client.Object)
	if !ok {
		return nil, ErrInvalidRedpandaClientObject
	}

	cluster, err := c.getCluster(ctx, o)
	if err != nil {
		return nil, err
	}

	if cluster != nil {
		return c.redpandaAdminForCluster(cluster)
	}

	if spec := c.getAdminSpec(o); spec != nil {
		return c.redpandaAdminForSpec(ctx, o.GetNamespace(), spec)
	}

	return nil, ErrInvalidRedpandaClientObject
}

func (c *Factory) SchemaRegistryClient(ctx context.Context, obj any) (*sr.Client, error) {
	// if we pass in a Redpanda cluster, just use it
	if cluster, ok := obj.(*redpandav1alpha2.Redpanda); ok {
		return c.schemaRegistryForCluster(cluster)
	}

	if profile, ok := obj.(*rpkconfig.RpkProfile); ok {
		return c.schemaRegistryForRPKProfile(profile)
	}

	o, ok := obj.(client.Object)
	if !ok {
		return nil, ErrInvalidSchemaRegistryClientObject
	}

	cluster, err := c.getCluster(ctx, o)
	if err != nil {
		return nil, err
	}

	if cluster != nil {
		return c.schemaRegistryForCluster(cluster)
	}

	if spec := c.getSchemaRegistrySpec(o); spec != nil {
		return c.schemaRegistryForSpec(ctx, o.GetNamespace(), spec)
	}

	return nil, ErrInvalidSchemaRegistryClientObject
}

func (c *Factory) Schemas(ctx context.Context, obj redpandav1alpha2.ClusterReferencingObject) (*schemas.Syncer, error) {
	schemaRegistryClient, err := c.SchemaRegistryClient(ctx, obj)
	if err != nil {
		return nil, err
	}

	return schemas.NewSyncer(schemaRegistryClient), nil
}

func (c *Factory) ACLs(ctx context.Context, obj redpandav1alpha2.ClusterReferencingObject, opts ...kgo.Opt) (*acls.Syncer, error) {
	kafkaClient, err := c.KafkaClient(ctx, obj, opts...)
	if err != nil {
		return nil, err
	}

	return acls.NewSyncer(kafkaClient), nil
}

func (c *Factory) Users(ctx context.Context, obj redpandav1alpha2.ClusterReferencingObject, opts ...kgo.Opt) (*users.Client, error) {
	kafkaClient, err := c.KafkaClient(ctx, obj, opts...)
	if err != nil {
		return nil, err
	}

	adminClient, err := c.RedpandaAdminClient(ctx, obj)
	if err != nil {
		return nil, err
	}

	return users.NewClient(ctx, c.Client, kadm.NewClient(kafkaClient), adminClient)
}

func (c *Factory) getCluster(ctx context.Context, obj client.Object) (*redpandav1alpha2.Redpanda, error) {
	o, ok := obj.(redpandav1alpha2.ClusterReferencingObject)
	if !ok {
		return nil, nil
	}

	if source := o.GetClusterSource(); source != nil { //nolint:nestif // ignore
		if ref := source.GetClusterRef(); ref != nil {
			var cluster redpandav1alpha2.Redpanda

			if err := c.Get(ctx, types.NamespacedName{Namespace: obj.GetNamespace(), Name: ref.Name}, &cluster); err != nil {
				if apierrors.IsNotFound(err) {
					return nil, ErrInvalidClusterRef
				}
				return nil, err
			}

			return &cluster, nil
		}
	}

	return nil, nil
}

func (c *Factory) getKafkaSpec(obj client.Object) *redpandav1alpha2.KafkaAPISpec {
	if o, ok := obj.(redpandav1alpha2.ClusterReferencingObject); ok {
		if source := o.GetClusterSource(); source != nil {
			if spec := source.GetKafkaAPISpec(); spec != nil {
				return spec
			}
		}
	}

	if o, ok := obj.(redpandav1alpha2.KafkaConnectedObject); ok {
		return o.GetKafkaAPISpec()
	}
	return nil
}

func (c *Factory) getKafkaMetricNamespace(obj client.Object) *string {
	if o, ok := obj.(redpandav1alpha2.KafkaConnectedObjectWithMetrics); ok {
		return o.GetMetricsNamespace()
	}
	return nil
}

func (c *Factory) getAdminSpec(obj client.Object) *redpandav1alpha2.AdminAPISpec {
	if o, ok := obj.(redpandav1alpha2.ClusterReferencingObject); ok {
		if source := o.GetClusterSource(); source != nil {
			return source.GetAdminAPISpec()
		}
	}

	return nil
}

func (c *Factory) getSchemaRegistrySpec(obj client.Object) *redpandav1alpha2.SchemaRegistrySpec {
	if o, ok := obj.(redpandav1alpha2.ClusterReferencingObject); ok {
		if source := o.GetClusterSource(); source != nil {
			return source.GetSchemaRegistrySpec()
		}
	}

	return nil
}

func (c *Factory) kafkaUserAuth() (kgo.Opt, error) {
	if c.userAuth != nil {
		auth := scram.Auth{
			User: c.userAuth.Username,
			Pass: c.userAuth.Password,
		}

		var mechanism sasl.Mechanism
		switch c.userAuth.Mechanism {
		case config.SASLMechanismScramSHA256:
			mechanism = auth.AsSha256Mechanism()
		case config.SASLMechanismScramSHA512:
			mechanism = auth.AsSha512Mechanism()
		default:
			return nil, ErrUnsupportedSASLMechanism
		}

		return kgo.SASL(mechanism), nil
	}

	return nil, nil
}
