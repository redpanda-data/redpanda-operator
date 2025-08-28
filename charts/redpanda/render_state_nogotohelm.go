// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

//go:build !gotohelm

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

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
)

var (
	ErrServerCertificateNotFound          = errors.New("server TLS certificate not found")
	ErrServerCertificatePublicKeyNotFound = errors.New("server TLS certificate does not contain a public key")

	ErrClientCertificateNotFound           = errors.New("client TLS certificate not found")
	ErrClientCertificatePublicKeyNotFound  = errors.New("client TLS certificate does not contain a public key")
	ErrClientCertificatePrivateKeyNotFound = errors.New("client TLS certificate does not contain a private key")

	ErrSASLSecretNotFound          = errors.New("users secret not found")
	ErrSASLSecretKeyNotFound       = errors.New("users secret key not found")
	ErrSASLSecretSuperuserNotFound = errors.New("users secret has no users")

	supportedSASLMechanisms = []string{
		"SCRAM-SHA-256", "SCRAM-SHA-512",
	}
)

// FetchSASLUsers attempts to locate an existing SASL users secret in the cluster.
// If found, it is used to populate the first user in the secret for use.
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
func RenderStateFromDot(dot *helmette.Dot, migrateFNs ...func(state *RenderState) error) (*RenderState, error) {
	state, err := renderStateFromDot(dot)
	if err != nil {
		return nil, err
	}

	for _, fn := range migrateFNs {
		if err := fn(state); err != nil {
			return nil, err
		}
	}

	return state, nil
}

// renderStateFromDot constructs a [RenderState] from the provided [helmette.Dot]
func renderStateFromDot(dot *helmette.Dot) (state *RenderState, err error) {
	defer func() {
		switch r := recover().(type) {
		case nil:
		case error:
			err = errors.Wrapf(r, "chart execution failed")
		default:
			err = errors.Newf("chart execution failed: %#v", r)
		}
	}()

	state = &RenderState{
		Release: &dot.Release,
		Files:   &dot.Files,
		Chart:   &dot.Chart,
		Values:  helmette.Unwrap[Values](dot.Values),
		Dot:     dot,
	}
	state.FetchBootstrapUser()
	state.FetchStatefulSetPodSelector()

	return
}

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
func (r *RenderState) KubeCTL() (*kube.Ctl, error) {
	return kube.FromRESTConfig(r.Dot.KubeConfig)
}

// RenderNodePools can be used to render node pools programmatically from Go.
func RenderNodePools(state *RenderState) (_ []*appsv1.StatefulSet, err error) {
	defer func() {
		switch r := recover().(type) {
		case nil:
		case error:
			err = errors.Wrapf(r, "chart execution failed")
		default:
			err = errors.Newf("chart execution failed: %#v", r)
		}
	}()

	return StatefulSets(state), nil
}

// RenderResources can be used to render non-nodepool resources programmatically from Go.
func RenderResources(state *RenderState) (_ []kube.Object, err error) {
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
		// filter out any helm hooks
		if annotations := resources[i].GetAnnotations(); annotations != nil {
			if _, isHook := annotations["helm.sh/hook"]; isHook {
				continue
			}
		}
		resources[j] = resources[i]
		j++
	}

	return resources[:j], nil
}
