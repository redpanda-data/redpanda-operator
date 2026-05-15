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
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"

	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/scram"
	"github.com/twmb/franz-go/pkg/sr"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	redpandaclient "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/client"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/internal/lifecycle"
	rendermulticluster "github.com/redpanda-data/redpanda-operator/operator/multicluster"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/tplutil"
)

// defaultedPoolSpec fetches a NodePool belonging to the StretchCluster from
// the given Kubernetes client, applies cluster and pool defaults, and returns
// its EmbeddedNodePoolSpec. Per-K8s-cluster fields (TLS, Listeners, ports) live
// on the NodePool spec, so building API clients requires the merged pool view.
func (c *Factory) defaultedPoolSpec(ctx context.Context, sc *redpandav1alpha2.StretchCluster, k8sClient client.Client) (*redpandav1alpha2.EmbeddedNodePoolSpec, error) {
	listCtx, listCancel := context.WithTimeout(ctx, lifecycle.RemoteCallTimeout)
	defer listCancel()

	var nodePoolList redpandav1alpha2.NodePoolList
	if err := k8sClient.List(listCtx, &nodePoolList, client.InNamespace(sc.Namespace)); err != nil {
		return nil, errors.Wrap(err, "listing NodePools")
	}

	defaultedClusterSpec := *sc.Spec.DeepCopy()
	defaultedClusterSpec.MergeDefaults()
	for i := range nodePoolList.Items {
		pool := &nodePoolList.Items[i]
		ref := pool.Spec.ClusterRef
		if !ref.IsStretchCluster() || ref.Name != sc.Name {
			continue
		}
		poolSpec := pool.Spec.EmbeddedNodePoolSpec.DeepCopy()
		poolSpec.MergeDefaultsFrom(&defaultedClusterSpec)
		return poolSpec, nil
	}
	return nil, fmt.Errorf("no NodePools found for StretchCluster %s/%s", sc.Namespace, sc.Name)
}

// redpandaAdminForStretchCluster builds an admin API client for a StretchCluster
// by deriving endpoints, TLS configuration, and authentication from the spec
// and associated Kubernetes secrets.
func (c *Factory) redpandaAdminForStretchCluster(ctx context.Context, sc *redpandav1alpha2.StretchCluster, clusterName string) (*rpadmin.AdminAPI, error) {
	k8sClient, err := c.GetClient(ctx, clusterName)
	if err != nil {
		return nil, errors.Wrap(err, "getting k8s client")
	}

	poolSpec, err := c.defaultedPoolSpec(ctx, sc, k8sClient)
	if err != nil {
		return nil, errors.Wrap(err, "resolving NodePool spec")
	}

	endpoints, err := c.stretchClusterEndpoints(ctx, sc, poolSpec.AdminPort())
	if err != nil {
		return nil, errors.Wrap(err, "discovering admin endpoints")
	}
	if len(endpoints) == 0 {
		return nil, fmt.Errorf("no admin API endpoints found for StretchCluster %s/%s", sc.Namespace, sc.Name)
	}

	var listener *redpandav1alpha2.StretchAPIListener
	if poolSpec.Listeners != nil {
		listener = poolSpec.Listeners.Admin
	}
	tlsConfig, err := c.stretchClusterListenerTLSConfig(ctx, sc, poolSpec, listener, k8sClient)
	if err != nil {
		return nil, errors.Wrap(err, "building TLS config")
	}

	username, password, err := c.stretchClusterAuth(ctx, sc, k8sClient)
	if err != nil {
		return nil, errors.Wrap(err, "reading auth credentials")
	}

	adminClient, err := redpandaclient.AdminClientForStretch(c.dialer, endpoints, username, password, tlsConfig, rpadmin.ClientTimeout(c.adminClientTimeout))
	if err != nil {
		return nil, errors.Wrap(err, "creating admin client")
	}

	return adminClient, nil
}

// kafkaForStretchCluster builds a Kafka client for a StretchCluster.
func (c *Factory) kafkaForStretchCluster(ctx context.Context, sc *redpandav1alpha2.StretchCluster, clusterName string, opts ...kgo.Opt) (*kgo.Client, error) {
	k8sClient, err := c.GetClient(ctx, clusterName)
	if err != nil {
		return nil, errors.Wrap(err, "getting k8s client")
	}

	poolSpec, err := c.defaultedPoolSpec(ctx, sc, k8sClient)
	if err != nil {
		return nil, errors.Wrap(err, "resolving NodePool spec")
	}

	brokers, err := c.stretchClusterEndpoints(ctx, sc, poolSpec.KafkaPort())
	if err != nil {
		return nil, errors.Wrap(err, "discovering kafka endpoints")
	}
	if len(brokers) == 0 {
		return nil, fmt.Errorf("no Kafka brokers found for StretchCluster %s/%s", sc.Namespace, sc.Name)
	}

	var listener *redpandav1alpha2.StretchAPIListener
	if poolSpec.Listeners != nil {
		listener = poolSpec.Listeners.Kafka
	}
	tlsConfig, err := c.stretchClusterListenerTLSConfig(ctx, sc, poolSpec, listener, k8sClient)
	if err != nil {
		return nil, errors.Wrap(err, "building TLS config")
	}

	clientOpts := append([]kgo.Opt{kgo.SeedBrokers(brokers...)}, opts...)
	if c.dialer != nil && tlsConfig != nil {
		clientOpts = append(clientOpts, kgo.Dialer(redpandaclient.WrapTLSDialer(c.dialer, tlsConfig)))
	} else if c.dialer != nil {
		clientOpts = append(clientOpts, kgo.Dialer(c.dialer))
	} else if tlsConfig != nil {
		clientOpts = append(clientOpts, kgo.DialTLSConfig(tlsConfig))
	}

	username, password, err := c.stretchClusterAuth(ctx, sc, k8sClient)
	if err != nil {
		return nil, errors.Wrap(err, "reading auth credentials")
	}
	if username != "" {
		clientOpts = append(clientOpts, kgo.SASL(scram.Auth{
			User: username,
			Pass: password,
		}.AsSha256Mechanism()))
	}

	return kgo.NewClient(clientOpts...)
}

// schemaRegistryForStretchCluster builds a SchemaRegistry client for a StretchCluster.
func (c *Factory) schemaRegistryForStretchCluster(ctx context.Context, sc *redpandav1alpha2.StretchCluster, clusterName string) (*sr.Client, error) {
	k8sClient, err := c.GetClient(ctx, clusterName)
	if err != nil {
		return nil, errors.Wrap(err, "getting k8s client")
	}

	poolSpec, err := c.defaultedPoolSpec(ctx, sc, k8sClient)
	if err != nil {
		return nil, errors.Wrap(err, "resolving NodePool spec")
	}

	endpoints, err := c.stretchClusterEndpoints(ctx, sc, poolSpec.SchemaRegistryPort())
	if err != nil {
		return nil, errors.Wrap(err, "discovering schema registry endpoints")
	}
	if len(endpoints) == 0 {
		return nil, fmt.Errorf("no schema registry endpoints found for StretchCluster %s/%s", sc.Namespace, sc.Name)
	}

	var listener *redpandav1alpha2.StretchAPIListener
	if poolSpec.Listeners != nil {
		listener = poolSpec.Listeners.SchemaRegistry
	}
	tlsConfig, err := c.stretchClusterListenerTLSConfig(ctx, sc, poolSpec, listener, k8sClient)
	if err != nil {
		return nil, errors.Wrap(err, "building TLS config")
	}

	scheme := "http"
	transport := &http.Transport{
		DialContext: c.dialer,
	}
	if tlsConfig != nil {
		scheme = "https"
		transport.TLSClientConfig = tlsConfig
	}

	urls := make([]string, len(endpoints))
	for i, ep := range endpoints {
		urls[i] = fmt.Sprintf("%s://%s", scheme, ep)
	}

	srOpts := []sr.ClientOpt{sr.URLs(urls...), sr.HTTPClient(&http.Client{Transport: transport})}

	username, password, err := c.stretchClusterAuth(ctx, sc, k8sClient)
	if err != nil {
		return nil, errors.Wrap(err, "reading auth credentials")
	}
	if username != "" {
		srOpts = append(srOpts, sr.BasicAuth(username, password))
	}

	return sr.NewClient(srOpts...)
}

// stretchClusterEndpoints lists NodePools referencing the given StretchCluster
// across all clusters known to the manager, and builds per-pod endpoint addresses
// for the given port.
func (c *Factory) stretchClusterEndpoints(ctx context.Context, sc *redpandav1alpha2.StretchCluster, port int32) ([]string, error) {
	var endpoints []string

	for _, clusterName := range c.mgr.GetClusterNames() {
		// Skip peers the probe has marked unreachable. Without this the List
		// below blocks at the kernel TCP dial budget (~30s) on every reconcile
		// that reaches the admin-client init phase, which is plenty to chain
		// reconciles past the 30s partition-handling SLA. The admin client
		// only needs reachable brokers; pods on a partitioned peer cluster
		// can't be reached anyway, so dropping them from the endpoint list
		// is the right behavior.
		if clusterName != mcmanager.LocalCluster && !c.mgr.IsClusterReachable(clusterName) {
			continue
		}
		k8sClient, err := c.GetClient(ctx, clusterName)
		if err != nil {
			return nil, errors.Wrapf(err, "getting client for cluster %s", clusterName)
		}

		listCtx, listCancel := context.WithTimeout(ctx, lifecycle.RemoteCallTimeout)
		var nodePoolList redpandav1alpha2.NodePoolList
		err = k8sClient.List(listCtx, &nodePoolList, client.InNamespace(sc.Namespace))
		listCancel()
		if err != nil {
			if clusterName != mcmanager.LocalCluster {
				// Treat a transient peer error the same as the probe
				// having flagged it: drop the peer's endpoints from this
				// reconcile and let the next round pick them up.
				continue
			}
			return nil, errors.Wrapf(err, "listing NodePools in cluster %s", clusterName)
		}

		for i := range nodePoolList.Items {
			pool := &nodePoolList.Items[i]
			ref := pool.Spec.ClusterRef
			if !ref.IsStretchCluster() || ref.Name != sc.Name {
				continue
			}
			for j := int32(0); j < pool.GetReplicas(); j++ {
				poolFullname := tplutil.CleanForK8s(sc.Name) + pool.Suffix()
				name := rendermulticluster.PerPodServiceName(poolFullname, j)
				endpoints = append(endpoints, fmt.Sprintf("%s.%s:%d", name, pool.GetNamespace(), port))
			}
		}
	}
	return endpoints, nil
}

// stretchClusterListenerTLSConfig builds a *tls.Config for a listener if TLS is
// enabled, reading the CA certificate from the shared root CA secret.
func (c *Factory) stretchClusterListenerTLSConfig(ctx context.Context, sc *redpandav1alpha2.StretchCluster, spec *redpandav1alpha2.EmbeddedNodePoolSpec, listener *redpandav1alpha2.StretchAPIListener, k8sClient client.Client) (*tls.Config, error) {
	tlsEnabled := false
	if listener != nil {
		tlsEnabled = listener.IsTLSEnabled(spec.TLS)
	}
	if !tlsEnabled {
		return nil, nil
	}

	certName := listener.TLS.GetCert()
	if certName == "" {
		certName = "default"
	}

	// CertificatesFor resolves the CA secret name and key based on the cert
	// type: operator-managed CA, user-provided SecretRef, or external IssuerRef.
	caSecretName, caKey, _ := spec.TLS.CertificatesFor(sc.Name, certName)
	var caSecret corev1.Secret
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: sc.Namespace, Name: caSecretName}, &caSecret); err != nil {
		return nil, errors.Wrapf(err, "reading CA secret %q", caSecretName)
	}

	caCert := caSecret.Data[caKey]
	if len(caCert) == 0 {
		// Fallback: try ca.crt then tls.crt.
		caCert = caSecret.Data["ca.crt"]
		if len(caCert) == 0 {
			caCert = caSecret.Data[corev1.TLSCertKey]
		}
	}

	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}

	if len(caCert) > 0 {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA certificate from secret %q", caSecretName)
		}
		tlsConfig.RootCAs = pool
	}

	if listener.TLS.RequiresClientAuth() {
		clientSecretName := spec.TLS.CertClientSecretName(sc.Name, certName)
		var clientSecret corev1.Secret
		if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: sc.Namespace, Name: clientSecretName}, &clientSecret); err != nil {
			return nil, errors.Wrapf(err, "reading client cert secret %q", clientSecretName)
		}

		cert, err := tls.X509KeyPair(clientSecret.Data[corev1.TLSCertKey], clientSecret.Data[corev1.TLSPrivateKeyKey])
		if err != nil {
			return nil, errors.Wrap(err, "parsing client certificate")
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	return tlsConfig, nil
}

// stretchClusterAuth reads the bootstrap user credentials from the bootstrap
// user secret if SASL is enabled.
func (c *Factory) stretchClusterAuth(ctx context.Context, sc *redpandav1alpha2.StretchCluster, k8sClient client.Client) (username, password string, _ error) {
	if !sc.Spec.Auth.IsSASLEnabled() {
		return "", "", nil
	}

	secretName := sc.BootstrapUserSecretName()
	var secret corev1.Secret
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: sc.Namespace, Name: secretName}, &secret); err != nil {
		return "", "", errors.Wrapf(err, "reading bootstrap user secret %q", secretName)
	}

	pw, ok := secret.Data[redpandav1alpha2.StretchClusterBootstrapPasswordKey]
	if !ok || len(pw) == 0 {
		return "", "", fmt.Errorf("bootstrap user secret %q has no password", secretName)
	}

	return redpandav1alpha2.StretchClusterBootstrapUsername, string(pw), nil
}
