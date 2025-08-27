// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_render_state.go.tpl
package redpanda

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/cockroachdb/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
	redpandav1alpha3 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha3"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
)

var (
	// +gotohelm:ignore=true
	ErrServerCertificateNotFound = errors.New("server TLS certificate not found")
	// +gotohelm:ignore=true
	ErrServerCertificatePublicKeyNotFound = errors.New("server TLS certificate does not contain a public key")

	// +gotohelm:ignore=true
	ErrClientCertificateNotFound = errors.New("client TLS certificate not found")
	// +gotohelm:ignore=true
	ErrClientCertificatePublicKeyNotFound = errors.New("client TLS certificate does not contain a public key")
	// +gotohelm:ignore=true
	ErrClientCertificatePrivateKeyNotFound = errors.New("client TLS certificate does not contain a private key")

	// +gotohelm:ignore=true
	ErrSASLSecretNotFound = errors.New("users secret not found")
	// +gotohelm:ignore=true
	ErrSASLSecretKeyNotFound = errors.New("users secret key not found")
	// +gotohelm:ignore=true
	ErrSASLSecretSuperuserNotFound = errors.New("users secret has no users")
	// +gotohelm:ignore=true
	supportedSASLMechanisms = []string{
		"SCRAM-SHA-256", "SCRAM-SHA-512",
	}
)

// RenderState contains contextual information about the current rendering of
// the chart.
type RenderState struct {
	// Metadata about the current Helm release.
	Release *helmette.Release
	// Files contains the static files that are part of the Helm chart.
	Files *helmette.Files
	// Chart is the helm chart being rendered.
	Chart *helmette.Chart

	// Values are the values used to render the chart.
	Values Values

	// BootstrapUserSecret is the Secret that may already exist in the cluster.
	BootstrapUserSecret *corev1.Secret
	// BootstrapPassword is the password of the bootstrap user, if it could be found.
	BootstrapUserPassword string

	// StatefulSetPodLabels contains the labels that may already exist for the statefulset pod template.
	StatefulSetPodLabels map[string]string
	// StatefulSetSelector contains the selector that may already exist for the statefulset.
	StatefulSetSelector map[string]string

	// Pools contains the list of NodePools that are being rendered.
	// TODO: move this to v1alpha2
	Pools []*redpandav1alpha3.NodePool

	// Dot is the underlying [helmette.Dot] that was used to construct this
	// RenderState.
	// TODO: remove this eventually once we get templating figured out.
	Dot *helmette.Dot
}

// FetchBootstrapUser attempts to locate an existing bootstrap user secret in
// the cluster. If found, it is stored in [RenderState.BootstrapUserSecret
func (r *RenderState) FetchBootstrapUser() {
	if r.Values.Auth.SASL == nil || !r.Values.Auth.SASL.Enabled || r.Values.Auth.SASL.BootstrapUser.SecretKeyRef != nil {
		return
	}

	secretName := fmt.Sprintf("%s-bootstrap-user", Fullname(r))

	// Some tools don't correctly set .Release.Upgrade (ArgoCD, gotohelm, helm
	// template) which has lead us to incorrectly re-generate the bootstrap
	// user password. Rather than gating, we always attempt a lookup as that's
	// likely the safest option. Though it's likely that Lookup will be
	// stubbed out in similar scenarios (helm template).
	// TODO: Should we try to detect invalid configurations, panic, and request
	// that a password be explicitly set?
	// See also: https://github.com/redpanda-data/helm-charts/issues/1596
	if existing, ok := helmette.Lookup[corev1.Secret](r.Dot, r.Release.Namespace, secretName); ok {
		// make any existing secret immutable
		existing.Immutable = ptr.To(true)
		r.BootstrapUserSecret = existing
		selector := r.Values.Auth.SASL.BootstrapUser.SecretKeySelector(Fullname(r))
		if data, found := existing.Data[selector.Key]; found {
			r.BootstrapUserPassword = string(data)
		}
	}
}

// FetchStatefulSetPodSelector attempts to locate an existing statefulset pod
// selector in the cluster. If found, it is stored in [RenderState.StatefulSetPodLabels
func (r *RenderState) FetchStatefulSetPodSelector() {
	// TODO: this may be broken now that we no longer fully distinguish between upgrades/installs
	// in controller applies
	// StatefulSets cannot change their selector. Use the existing one even if it's broken.
	// New installs will get better selectors.
	if r.Release.IsUpgrade {
		if existing, ok := helmette.Lookup[appsv1.StatefulSet](r.Dot, r.Release.Namespace, Fullname(r)); ok && len(existing.Spec.Template.ObjectMeta.Labels) > 0 {
			r.StatefulSetPodLabels = existing.Spec.Template.ObjectMeta.Labels
			r.StatefulSetSelector = existing.Spec.Selector.MatchLabels
		}
	}
}

// FetchSASLUsers attempts to locate an existing SASL users secret in the cluster.
// If found, it is used to populate the first user in the secret for use.
// +gotohelm:ignore=true
func (r *RenderState) FetchSASLUsers() (username, password, mechanism string, err error) {
	saslUsers := SecretSASLUsers(r)
	saslUsersError := func(err error) error {
		return fmt.Errorf("error fetching SASL authentication for %s/%s: %w", saslUsers.Namespace, saslUsers.Name, err)
	}

	if saslUsers != nil {
		// read from the server since we're assuming all the resources
		// have already been created
		users, found, lookupErr := helmette.SafeLookup[corev1.Secret](r.Dot, saslUsers.Namespace, saslUsers.Name)
		if lookupErr != nil {
			err = saslUsersError(lookupErr)
			return
		}

		if !found {
			err = saslUsersError(ErrSASLSecretNotFound)
			return
		}

		data, found := users.Data["users.txt"]
		if !found {
			err = saslUsersError(ErrSASLSecretKeyNotFound)
			return
		}

		username, password, mechanism = firstUser(data)
		if username == "" {
			err = saslUsersError(ErrSASLSecretSuperuserNotFound)
			return
		}
	}

	return
}

// RenderStateFromDot constructs a [RenderState] from the provided [helmette.Dot].
// +gotohelm:ignore=true
func RenderStateFromDot(dot *helmette.Dot, migrateFNs ...func(values Values) error) (*RenderState, error) {
	state := renderStateFromDot(dot)

	for _, fn := range migrateFNs {
		if err := fn(state.Values); err != nil {
			return nil, err
		}
	}

	return state, nil
}

// renderStateFromDot constructs a [RenderState] from the provided [helmette.Dot]
// +gotohelm:ignore=true
func renderStateFromDot(dot *helmette.Dot) *RenderState {
	state := &RenderState{
		Release: &dot.Release,
		Files:   &dot.Files,
		Chart:   &dot.Chart,
		Values:  helmette.Unwrap[Values](dot.Values),
		Dot:     dot,
	}
	state.FetchBootstrapUser()
	state.FetchStatefulSetPodSelector()

	return state
}

// +gotohelm:ignore=true
func firstUser(data []byte) (user string, password string, mechanism string) {
	file := string(data)

	for _, line := range strings.Split(file, "\n") {
		tokens := strings.Split(line, ":")

		switch len(tokens) {
		case 2:
			return tokens[0], tokens[1], string(DefaultSASLMechanism)

		case 3:
			if !slices.Contains(supportedSASLMechanisms, tokens[2]) {
				continue
			}

			return tokens[0], tokens[1], tokens[2]

		default:
			continue
		}
	}

	return
}

// TLSConfig constructs a tls.Config for the given internal listener.
// +gotohelm:ignore=true
func (r *RenderState) TLSConfig(listener InternalTLS) (*tls.Config, error) {
	namespace := r.Release.Namespace
	serverName := InternalDomain(r)

	rootCertName, rootCertKey, clientCertName := certificatesFor(r, listener.Cert)

	serverTLSError := func(err error) error {
		return fmt.Errorf("error fetching server root CA %s/%s: %w", namespace, rootCertName, err)
	}
	clientTLSError := func(err error) error {
		return fmt.Errorf("error fetching client certificate default/%s: %w", clientCertName, err)
	}

	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: serverName}

	serverCert, found, lookupErr := helmette.SafeLookup[corev1.Secret](r.Dot, namespace, rootCertName)
	if lookupErr != nil {
		return nil, serverTLSError(lookupErr)
	}

	if !found {
		return nil, serverTLSError(ErrServerCertificateNotFound)
	}

	serverPublicKey, found := serverCert.Data[rootCertKey]
	if !found {
		return nil, serverTLSError(ErrServerCertificatePublicKeyNotFound)
	}

	block, _ := pem.Decode(serverPublicKey)
	serverParsedCertificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, serverTLSError(fmt.Errorf("unable to parse public key %w", err))
	}
	pool := x509.NewCertPool()
	pool.AddCert(serverParsedCertificate)

	tlsConfig.RootCAs = pool

	if listener.RequireClientAuth {
		clientCert, found, lookupErr := helmette.SafeLookup[corev1.Secret](r.Dot, namespace, clientCertName)
		if lookupErr != nil {
			return nil, clientTLSError(lookupErr)
		}

		if !found {
			return nil, clientTLSError(ErrServerCertificateNotFound)
		}

		// we always use tls.crt for client certs
		clientPublicKey, found := clientCert.Data[corev1.TLSCertKey]
		if !found {
			return nil, clientTLSError(ErrClientCertificatePublicKeyNotFound)
		}

		clientPrivateKey, found := clientCert.Data[corev1.TLSPrivateKeyKey]
		if !found {
			return nil, clientTLSError(ErrClientCertificatePrivateKeyNotFound)
		}

		clientKey, err := tls.X509KeyPair(clientPublicKey, clientPrivateKey)
		if err != nil {
			return nil, clientTLSError(fmt.Errorf("unable to parse public and private key %w", err))
		}

		tlsConfig.Certificates = []tls.Certificate{clientKey}
	}

	return tlsConfig, nil
}

// +gotohelm:ignore=true
func certificatesFor(state *RenderState, cert string) (certSecret, certKey, clientSecret string) {
	name := Fullname(state)

	// default to cert manager issued names and tls.crt which is
	// where cert-manager outputs the root CA
	certKey = corev1.TLSCertKey
	certSecret = fmt.Sprintf("%s-%s-root-certificate", name, cert)
	clientSecret = fmt.Sprintf("%s-client", name)

	if certificate, ok := state.Values.TLS.Certs[cert]; ok {
		// if this references a non-enabled certificate, just return
		// the default cert-manager issued names
		if certificate.Enabled != nil && !*certificate.Enabled {
			return certSecret, certKey, clientSecret
		}

		if certificate.ClientSecretRef != nil {
			clientSecret = certificate.ClientSecretRef.Name
		}
		if certificate.SecretRef != nil {
			certSecret = certificate.SecretRef.Name
			if certificate.CAEnabled {
				certKey = "ca.crt"
			}
		}
	}
	return certSecret, certKey, clientSecret
}

// KubeCTL constructs a kube.Ctl from the RenderState's kubeconfig.
// +gotohelm:ignore=true
func (r *RenderState) KubeCTL() (*kube.Ctl, error) {
	return kube.FromRESTConfig(r.Dot.KubeConfig)
}

// RenderNodePools can be used to render node pools programmatically from Go.
// +gotohelm:ignore=true
func RenderNodePools(state *RenderState) (sets []*appsv1.StatefulSet, err error) {
	defer func() {
		switch r := recover().(type) {
		case nil:
		case error:
			err = errors.Wrapf(r, "chart execution failed")
		default:
			err = errors.Newf("chart execution failed: %#v", r)
		}
	}()

	sets = StatefulSets(state)
	return
}

// RenderResources can be used to render non-nodepool resources programmatically from Go.
// +gotohelm:ignore=true
func RenderResources(state *RenderState) (manifests []kube.Object, err error) {
	defer func() {
		switch r := recover().(type) {
		case nil:
		case error:
			err = errors.Wrapf(r, "chart execution failed")
		default:
			err = errors.Newf("chart execution failed: %#v", r)
		}
	}()

	resources := renderResources(state)

	// the renderer is expected to return nil interfaces.
	// In the helm world, these nils are filtered out by
	// _shims.render-manifests.
	j := 0
	for i := range resources {
		// Handle the nil unboxing issue.
		if reflect.ValueOf(resources[i]).IsNil() {
			continue
		}
		resources[j] = resources[i]
		j++
	}

	manifests = resources[:j]
	return
}
