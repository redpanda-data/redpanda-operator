// Copyright 2026 Redpanda Data, Inc.
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
	"time"

	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/redpanda-data/console/backend/pkg/config"
	rpkconfig "github.com/redpanda-data/redpanda/src/go/rpk/pkg/config"
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
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	redpanda "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/client"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/acls"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/roles"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/schemas"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/shadow"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/users"
	"github.com/redpanda-data/redpanda-operator/pkg/ir"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
	pkgsecrets "github.com/redpanda-data/redpanda-operator/pkg/secrets"
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
	// KafkaClientForCluster is the same as KafkaClient but it takes a kubernetes cluster name.
	KafkaClientForCluster(ctx context.Context, object any, clusterName string, opts ...kgo.Opt) (*kgo.Client, error)

	// RedpandaAdminClient initializes a rpadmin.AdminAPI client based on the spec of the passed in struct.
	// The struct *must* either be an RPK profile, Redpanda CR, or implement either the v1alpha2.AdminConnectedObject interface
	// or the v1alpha2.ClusterReferencingObject interface to properly initialize. Callers should call Close on the returned *rpadmin.AdminAPI
	// to ensure any idle connections in the underlying transport are closed.
	RedpandaAdminClient(ctx context.Context, object any) (*rpadmin.AdminAPI, error)
	// RedpandaAdminClientForCluster is the same as RedpandaAdminClient but it takes a kubernetes cluster name.
	RedpandaAdminClientForCluster(ctx context.Context, object any, clusterName string) (*rpadmin.AdminAPI, error)

	// SchemaRegistryClient initializes an sr.Client based on the spec of the passed in struct.
	// The struct *must* either be an RPK profile, Redpanda CR, or implement either the v1alpha2.SchemaRegistryConnectedObject interface
	// or the v1alpha2.ClusterReferencingObject interface to properly initialize.
	SchemaRegistryClient(ctx context.Context, object any) (*sr.Client, error)
	// SchemaRegistryClientForCluster is the same as SchemaRegistryClient but it takes a kubernetes cluster name.
	SchemaRegistryClientForCluster(ctx context.Context, object any, clusterName string) (*sr.Client, error)

	// ACLs returns a high-level client for synchronizing ACLs. Callers should always call Close on the returned *acls.Syncer, or it will leak
	// goroutines.
	ACLs(ctx context.Context, object redpandav1alpha2.ClusterReferencingObject, opts ...kgo.Opt) (*acls.Syncer, error)
	// ACLsForCluster is the same as ACLs but it takes a kubernetes cluster name
	ACLsForCluster(ctx context.Context, object redpandav1alpha2.ClusterReferencingObject, clusterName string, opts ...kgo.Opt) (*acls.Syncer, error)

	// Users returns a high-level client for managing users. Callers should always call Close on the returned *users.Client, or it will leak
	// goroutines.
	Users(ctx context.Context, object redpandav1alpha2.ClusterReferencingObject, opts ...kgo.Opt) (*users.Client, error)
	// UsersForCluster is the same as Users but it takes a kubernetes cluster name
	UsersForCluster(ctx context.Context, object redpandav1alpha2.ClusterReferencingObject, clusterName string, opts ...kgo.Opt) (*users.Client, error)

	// Roles returns a high-level client for managing roles. Callers should always call Close on the returned *roles.Client, or it will leak
	// goroutines.
	Roles(ctx context.Context, object redpandav1alpha2.ClusterReferencingObject) (*roles.Client, error)
	// RolesForCluster is the same as Roles but it takes a kubernetes cluster name
	RolesForCluster(ctx context.Context, object redpandav1alpha2.ClusterReferencingObject, clusterName string) (*roles.Client, error)

	// Schemas returns a high-level client for synchronizing Schemas.
	Schemas(ctx context.Context, object redpandav1alpha2.ClusterReferencingObject) (*schemas.Syncer, error)
	// SchemasForCluster is the same as Schemas but it takes a kubernetes cluster name
	SchemasForCluster(ctx context.Context, object redpandav1alpha2.ClusterReferencingObject, clusterName string) (*schemas.Syncer, error)

	// ShadowLinksForCluster returns a high-level client for synchronizing ShadowLinks.
	ShadowLinks(ctx context.Context, object redpandav1alpha2.RemoteClusterReferencingObject) (*shadow.Syncer, error)
	// ShadowLinksForCluster is the same as ShadowLinks but it takes a kubernetes cluster name
	ShadowLinksForCluster(ctx context.Context, object redpandav1alpha2.RemoteClusterReferencingObject, clusterName string) (*shadow.Syncer, error)

	// RemoteClusterSettings returns the ShadowLink connection configuration for an object.
	RemoteClusterSettings(ctx context.Context, object redpandav1alpha2.RemoteClusterReferencingObject) (shadow.RemoteClusterSettings, error)
	// RemoteClusterSettingsForCluster is the same as RemoteClusterSettings but it takes a kubernetes cluster name
	RemoteClusterSettingsForCluster(ctx context.Context, object redpandav1alpha2.RemoteClusterReferencingObject, clusterName string) (shadow.RemoteClusterSettings, error)
}

type Factory struct {
	mgr multicluster.Manager
	fs  afero.Fs

	adminClientTimeout time.Duration
	dialer             redpanda.DialContextFunc
	userAuth           *UserAuth
	secretExpander     *pkgsecrets.CloudExpander
}

var _ ClientFactory = (*Factory)(nil)

func NewFactory(mgr multicluster.Manager, expander *pkgsecrets.CloudExpander) *Factory {
	return &Factory{
		mgr:                mgr,
		fs:                 afero.NewOsFs(),
		secretExpander:     expander,
		adminClientTimeout: 10 * time.Second,
	}
}

func NewRPKOnlyFactory() *Factory {
	return NewFactory(nil, nil)
}

func (c *Factory) GetClient(ctx context.Context, clusterName string) (client.Client, error) {
	cluster, err := c.mgr.GetCluster(ctx, clusterName)
	if err != nil {
		return nil, err
	}
	return cluster.GetClient(), nil
}

func (c *Factory) GetConfig(ctx context.Context, clusterName string) (*rest.Config, error) {
	cluster, err := c.mgr.GetCluster(ctx, clusterName)
	if err != nil {
		return nil, err
	}
	return cluster.GetConfig(), nil
}

func (c *Factory) WithDialer(dialer redpanda.DialContextFunc) *Factory {
	return &Factory{
		mgr:                c.mgr,
		userAuth:           c.userAuth,
		fs:                 c.fs,
		dialer:             dialer,
		adminClientTimeout: c.adminClientTimeout,
	}
}

func (c *Factory) WithAdminClientTimeout(timeout time.Duration) *Factory {
	return &Factory{
		mgr:                c.mgr,
		userAuth:           c.userAuth,
		fs:                 c.fs,
		dialer:             c.dialer,
		adminClientTimeout: timeout,
	}
}

func (c *Factory) WithFS(fs afero.Fs) *Factory {
	return &Factory{
		mgr:                c.mgr,
		userAuth:           c.userAuth,
		dialer:             c.dialer,
		fs:                 fs,
		adminClientTimeout: c.adminClientTimeout,
	}
}

func (c *Factory) WithUserAuth(userAuth *UserAuth) *Factory {
	return &Factory{
		mgr:                c.mgr,
		dialer:             c.dialer,
		fs:                 c.fs,
		userAuth:           userAuth,
		adminClientTimeout: c.adminClientTimeout,
	}
}

func (c *Factory) KafkaClientForCluster(ctx context.Context, obj any, clusterName string, opts ...kgo.Opt) (*kgo.Client, error) {
	// if we pass in a Redpanda cluster, just use it
	if cluster, ok := obj.(*redpandav1alpha2.Redpanda); ok {
		return c.kafkaForCluster(ctx, cluster, clusterName, opts...)
	}

	if cluster, ok := obj.(*vectorizedv1alpha1.Cluster); ok {
		return c.kafkaForV1Cluster(ctx, cluster, clusterName)
	}

	if profile, ok := obj.(*rpkconfig.RpkProfile); ok {
		return c.kafkaForRPKProfile(profile, opts...)
	}

	o, ok := obj.(client.Object)
	if !ok {
		return nil, ErrInvalidKafkaClientObject
	}

	cluster, err := c.getV2Cluster(ctx, o, clusterName)
	if err != nil {
		return nil, err
	}

	if cluster != nil {
		return c.kafkaForCluster(ctx, cluster, clusterName, opts...)
	}

	v1Cluster, err := c.getV1Cluster(ctx, o, clusterName)
	if err != nil {
		return nil, err
	}

	if v1Cluster != nil {
		return c.kafkaForV1Cluster(ctx, v1Cluster, clusterName, opts...)
	}

	if spec := c.getKafkaSpec(o); spec != nil {
		return c.kafkaForSpec(ctx, c.getKafkaMetricNamespace(o), spec, clusterName, opts...)
	}

	return nil, ErrInvalidKafkaClientObject
}

func (c *Factory) KafkaClient(ctx context.Context, obj any, opts ...kgo.Opt) (*kgo.Client, error) {
	return c.KafkaClientForCluster(ctx, obj, mcmanager.LocalCluster, opts...)
}

func (c *Factory) RedpandaAdminClientForCluster(ctx context.Context, obj any, clusterName string) (*rpadmin.AdminAPI, error) {
	// if we pass in a Redpanda cluster, just use it
	if cluster, ok := obj.(*redpandav1alpha2.Redpanda); ok {
		return c.redpandaAdminForCluster(ctx, cluster, clusterName)
	}

	if cluster, ok := obj.(*vectorizedv1alpha1.Cluster); ok {
		return c.redpandaAdminForV1Cluster(ctx, cluster, clusterName)
	}

	if profile, ok := obj.(*rpkconfig.RpkProfile); ok {
		return c.redpandaAdminForRPKProfile(profile)
	}

	o, ok := obj.(client.Object)
	if !ok {
		return nil, ErrInvalidRedpandaClientObject
	}

	cluster, err := c.getV2Cluster(ctx, o, clusterName)
	if err != nil {
		return nil, err
	}

	if cluster != nil {
		return c.redpandaAdminForCluster(ctx, cluster, clusterName)
	}

	v1Cluster, err := c.getV1Cluster(ctx, o, clusterName)
	if err != nil {
		return nil, err
	}

	if v1Cluster != nil {
		return c.redpandaAdminForV1Cluster(ctx, v1Cluster, clusterName)
	}

	if spec := c.getAdminSpec(o); spec != nil {
		return c.redpandaAdminForSpec(ctx, spec, clusterName)
	}

	return nil, ErrInvalidRedpandaClientObject
}

func (c *Factory) RedpandaAdminClient(ctx context.Context, obj any) (*rpadmin.AdminAPI, error) {
	return c.RedpandaAdminClientForCluster(ctx, obj, mcmanager.LocalCluster)
}

func (c *Factory) SchemaRegistryClientForCluster(ctx context.Context, obj any, clusterName string) (*sr.Client, error) {
	// if we pass in a Redpanda cluster, just use it
	if cluster, ok := obj.(*redpandav1alpha2.Redpanda); ok {
		return c.schemaRegistryForCluster(ctx, cluster, clusterName)
	}

	if cluster, ok := obj.(*vectorizedv1alpha1.Cluster); ok {
		return c.schemaRegistryForV1Cluster(ctx, cluster, clusterName)
	}

	if profile, ok := obj.(*rpkconfig.RpkProfile); ok {
		return c.schemaRegistryForRPKProfile(profile)
	}

	o, ok := obj.(client.Object)
	if !ok {
		return nil, ErrInvalidSchemaRegistryClientObject
	}

	cluster, err := c.getV2Cluster(ctx, o, clusterName)
	if err != nil {
		return nil, err
	}

	if cluster != nil {
		return c.schemaRegistryForCluster(ctx, cluster, clusterName)
	}

	v1Cluster, err := c.getV1Cluster(ctx, o, clusterName)
	if err != nil {
		return nil, err
	}

	if v1Cluster != nil {
		return c.schemaRegistryForV1Cluster(ctx, v1Cluster, clusterName)
	}

	if spec := c.getSchemaRegistrySpec(o); spec != nil {
		return c.schemaRegistryForSpec(ctx, spec, clusterName)
	}

	return nil, ErrInvalidSchemaRegistryClientObject
}

func (c *Factory) SchemaRegistryClient(ctx context.Context, obj any) (*sr.Client, error) {
	return c.SchemaRegistryClientForCluster(ctx, obj, mcmanager.LocalCluster)
}

func (c *Factory) SchemasForCluster(ctx context.Context, obj redpandav1alpha2.ClusterReferencingObject, clusterName string) (*schemas.Syncer, error) {
	schemaRegistryClient, err := c.SchemaRegistryClient(ctx, obj)
	if err != nil {
		return nil, err
	}

	return schemas.NewSyncer(schemaRegistryClient), nil
}

func (c *Factory) Schemas(ctx context.Context, obj redpandav1alpha2.ClusterReferencingObject) (*schemas.Syncer, error) {
	return c.SchemasForCluster(ctx, obj, mcmanager.LocalCluster)
}

func (c *Factory) ShadowLinksForCluster(ctx context.Context, obj redpandav1alpha2.RemoteClusterReferencingObject, clusterName string) (*shadow.Syncer, error) {
	adminClient, err := c.RedpandaAdminClient(ctx, obj)
	if err != nil {
		return nil, err
	}

	return shadow.NewSyncer(adminClient), nil
}

func (c *Factory) ShadowLinks(ctx context.Context, obj redpandav1alpha2.RemoteClusterReferencingObject) (*shadow.Syncer, error) {
	return c.ShadowLinksForCluster(ctx, obj, mcmanager.LocalCluster)
}

func (c *Factory) ACLsForCluster(ctx context.Context, obj redpandav1alpha2.ClusterReferencingObject, clusterName string, opts ...kgo.Opt) (*acls.Syncer, error) {
	kafkaClient, err := c.KafkaClient(ctx, obj, opts...)
	if err != nil {
		return nil, err
	}

	return acls.NewSyncer(kafkaClient), nil
}

func (c *Factory) ACLs(ctx context.Context, obj redpandav1alpha2.ClusterReferencingObject, opts ...kgo.Opt) (*acls.Syncer, error) {
	return c.ACLsForCluster(ctx, obj, mcmanager.LocalCluster, opts...)
}

func (c *Factory) UsersForCluster(ctx context.Context, obj redpandav1alpha2.ClusterReferencingObject, clusterName string, opts ...kgo.Opt) (*users.Client, error) {
	client, err := c.GetClient(ctx, clusterName)
	if err != nil {
		return nil, err
	}

	kafkaClient, err := c.KafkaClient(ctx, obj, opts...)
	if err != nil {
		return nil, err
	}

	adminClient, err := c.RedpandaAdminClient(ctx, obj)
	if err != nil {
		return nil, err
	}

	return users.NewClient(ctx, client, kadm.NewClient(kafkaClient), adminClient)
}

func (c *Factory) Users(ctx context.Context, obj redpandav1alpha2.ClusterReferencingObject, opts ...kgo.Opt) (*users.Client, error) {
	return c.UsersForCluster(ctx, obj, mcmanager.LocalCluster, opts...)
}

func (c *Factory) RolesForCluster(ctx context.Context, obj redpandav1alpha2.ClusterReferencingObject, clusterName string) (*roles.Client, error) {
	adminClient, err := c.RedpandaAdminClient(ctx, obj)
	if err != nil {
		return nil, err
	}

	return roles.NewClient(ctx, adminClient)
}

func (c *Factory) Roles(ctx context.Context, obj redpandav1alpha2.ClusterReferencingObject) (*roles.Client, error) {
	return c.RolesForCluster(ctx, obj, mcmanager.LocalCluster)
}

func (c *Factory) RemoteClusterSettingsForCluster(ctx context.Context, obj redpandav1alpha2.RemoteClusterReferencingObject, clusterName string) (shadow.RemoteClusterSettings, error) {
	var settings shadow.RemoteClusterSettings

	o, ok := obj.(client.Object)
	if !ok {
		return settings, ErrInvalidKafkaClientObject
	}

	cluster, err := c.getRemoteV2Cluster(ctx, o, clusterName)
	if err != nil {
		return settings, err
	}

	if cluster != nil {
		return c.remoteClusterSettingsForCluster(ctx, cluster, clusterName)
	}

	v1Cluster, err := c.getRemoteV1Cluster(ctx, o, clusterName)
	if err != nil {
		return settings, err
	}

	if v1Cluster != nil {
		return c.remoteClusterSettingsForV1Cluster(ctx, v1Cluster, clusterName)
	}

	if spec := c.getRemoteKafkaSpec(o); spec != nil {
		return c.remoteClusterSettingsForSpec(ctx, spec, clusterName)
	}

	return settings, ErrInvalidKafkaClientObject
}

func (c *Factory) RemoteClusterSettings(ctx context.Context, obj redpandav1alpha2.RemoteClusterReferencingObject) (shadow.RemoteClusterSettings, error) {
	return c.RemoteClusterSettingsForCluster(ctx, obj, mcmanager.LocalCluster)
}

func (c *Factory) getV1Cluster(ctx context.Context, obj client.Object, clusterName string) (*vectorizedv1alpha1.Cluster, error) {
	o, ok := obj.(redpandav1alpha2.ClusterReferencingObject)
	if !ok {
		return nil, nil
	}

	client, err := c.GetClient(ctx, clusterName)
	if err != nil {
		return nil, err
	}

	if source := o.GetClusterSource(); source != nil { //nolint:nestif // ignore
		if ref := source.GetClusterRef(); ref != nil && ref.IsV1() {
			var cluster vectorizedv1alpha1.Cluster

			if err := client.Get(ctx, types.NamespacedName{Namespace: obj.GetNamespace(), Name: ref.Name}, &cluster); err != nil {
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

func (c *Factory) getRemoteV1Cluster(ctx context.Context, obj client.Object, clusterName string) (*vectorizedv1alpha1.Cluster, error) {
	o, ok := obj.(redpandav1alpha2.RemoteClusterReferencingObject)
	if !ok {
		return nil, nil
	}
	if source := o.GetRemoteClusterSource(); source != nil { //nolint:nestif // ignore
		if ref := source.GetClusterRef(); ref != nil && ref.IsV1() {
			var cluster vectorizedv1alpha1.Cluster

			client, err := c.GetClient(ctx, clusterName)
			if err != nil {
				return nil, err
			}

			if err := client.Get(ctx, types.NamespacedName{Namespace: obj.GetNamespace(), Name: ref.Name}, &cluster); err != nil {
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

func (c *Factory) getV2Cluster(ctx context.Context, obj client.Object, clusterName string) (*redpandav1alpha2.Redpanda, error) {
	o, ok := obj.(redpandav1alpha2.ClusterReferencingObject)
	if !ok {
		return nil, nil
	}

	if source := o.GetClusterSource(); source != nil { //nolint:nestif // ignore
		if ref := source.GetClusterRef(); ref != nil && ref.IsV2() {
			var cluster redpandav1alpha2.Redpanda

			client, err := c.GetClient(ctx, clusterName)
			if err != nil {
				return nil, err
			}

			if err := client.Get(ctx, types.NamespacedName{Namespace: obj.GetNamespace(), Name: ref.Name}, &cluster); err != nil {
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

func (c *Factory) getRemoteV2Cluster(ctx context.Context, obj client.Object, clusterName string) (*redpandav1alpha2.Redpanda, error) {
	o, ok := obj.(redpandav1alpha2.RemoteClusterReferencingObject)
	if !ok {
		return nil, nil
	}

	if source := o.GetRemoteClusterSource(); source != nil { //nolint:nestif // ignore
		if ref := source.GetClusterRef(); ref != nil && ref.IsV2() {
			var cluster redpandav1alpha2.Redpanda

			client, err := c.GetClient(ctx, clusterName)
			if err != nil {
				return nil, err
			}

			if err := client.Get(ctx, types.NamespacedName{Namespace: obj.GetNamespace(), Name: ref.Name}, &cluster); err != nil {
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

func (c *Factory) getKafkaSpec(obj client.Object) *ir.KafkaAPISpec {
	if o, ok := obj.(redpandav1alpha2.ClusterReferencingObject); ok {
		if source := o.GetClusterSource(); source != nil {
			if spec := source.GetKafkaAPISpec(); spec != nil {
				return redpandav1alpha2.ConvertKafkaAPISpecToIR(obj.GetNamespace(), spec)
			}
		}
	}

	if o, ok := obj.(redpandav1alpha2.KafkaConnectedObject); ok {
		return redpandav1alpha2.ConvertKafkaAPISpecToIR(o.GetNamespace(), o.GetKafkaAPISpec())
	}
	return nil
}

func (c *Factory) getRemoteKafkaSpec(obj client.Object) *ir.KafkaAPISpec {
	if o, ok := obj.(redpandav1alpha2.RemoteClusterReferencingObject); ok {
		if source := o.GetRemoteClusterSource(); source != nil {
			if spec := source.GetKafkaAPISpec(); spec != nil {
				return redpandav1alpha2.ConvertKafkaAPISpecToIR(obj.GetNamespace(), spec)
			}
		}
	}

	if o, ok := obj.(redpandav1alpha2.KafkaConnectedObject); ok {
		return redpandav1alpha2.ConvertKafkaAPISpecToIR(o.GetNamespace(), o.GetKafkaAPISpec())
	}
	return nil
}

func (c *Factory) getKafkaMetricNamespace(obj client.Object) *string {
	if o, ok := obj.(redpandav1alpha2.KafkaConnectedObjectWithMetrics); ok {
		return o.GetMetricsNamespace()
	}
	return nil
}

func (c *Factory) getAdminSpec(obj client.Object) *ir.AdminAPISpec {
	if o, ok := obj.(redpandav1alpha2.ClusterReferencingObject); ok {
		if source := o.GetClusterSource(); source != nil {
			return redpandav1alpha2.ConvertAdminAPISpecToIR(obj.GetNamespace(), source.GetAdminAPISpec())
		}
	}

	return nil
}

func (c *Factory) getSchemaRegistrySpec(obj client.Object) *ir.SchemaRegistrySpec {
	if o, ok := obj.(redpandav1alpha2.ClusterReferencingObject); ok {
		if source := o.GetClusterSource(); source != nil {
			return redpandav1alpha2.ConvertSchemaRegistrySpecToIR(o.GetNamespace(), source.GetSchemaRegistrySpec())
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
