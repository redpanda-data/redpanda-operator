// Copyright 2026 Redpanda Data, Inc.
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
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/common-go/kube"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8sapierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
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
	ctl, ctlErr := r.KubeCTL()
	if ctlErr != nil {
		err = ctlErr
		return
	}

	saslUsers := SecretSASLUsers(r)
	saslUsersError := func(err error) error {
		return fmt.Errorf("error fetching SASL authentication for %s/%s: %w", saslUsers.Namespace, saslUsers.Name, err)
	}

	if saslUsers != nil {
		// read from the server since we're assuming all the resources
		// have already been created
		var users corev1.Secret
		lookupErr := ctl.Get(context.TODO(), types.NamespacedName{Name: saslUsers.Name, Namespace: saslUsers.Namespace}, &users)
		if lookupErr != nil {
			if k8sapierrors.IsNotFound(lookupErr) {
				err = saslUsersError(ErrSASLSecretNotFound)
				return
			}
			err = saslUsersError(lookupErr)
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
	ctl, err := r.KubeCTL()
	if err != nil {
		return nil, err
	}

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

	var serverCert corev1.Secret
	lookupErr := ctl.Get(context.TODO(), types.NamespacedName{Name: rootCertName, Namespace: namespace}, &serverCert)
	if lookupErr != nil {
		if k8sapierrors.IsNotFound(lookupErr) {
			return nil, serverTLSError(ErrServerCertificateNotFound)
		}
		return nil, serverTLSError(lookupErr)
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
		var clientCert corev1.Secret
		lookupErr := ctl.Get(context.TODO(), types.NamespacedName{Name: clientCertName, Namespace: namespace}, &clientCert)
		if lookupErr != nil {
			if k8sapierrors.IsNotFound(lookupErr) {
				return nil, clientTLSError(ErrClientCertificateNotFound)
			}
			return nil, clientTLSError(lookupErr)
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

func certificatesFor(state *RenderState, name string) (certSecret, certKey, clientSecret string) {
	cert, ok := state.Values.TLS.Certs[name]
	if !ok || !ptr.Deref(cert.Enabled, true) {
		// TODO this isn't correct but it matches historical behavior.
		fullname := Fullname(state)
		certSecret = fmt.Sprintf("%s-%s-root-certificate", fullname, name)
		clientSecret = fmt.Sprintf("%s-default-client-cert", fullname)

		return certSecret, corev1.TLSCertKey, clientSecret
	}

	ref := cert.CASecretRef(state, name)
	return ref.LocalObjectReference.Name, ref.Key, cert.ClientSecretName(state, name)
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
