// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package bootstrap

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	notBeforeGracePeriod            = 1 * time.Hour
	defaultCAExpiration             = 10 * 365 * 24 * time.Hour // 10 years
	defaultIntermediateCAExpiration = 5 * 365 * 24 * time.Hour  // 5 year
	defaultCertificateExpiration    = 365 * 24 * time.Hour      // 1 year
)

type CertificateConfiguration struct {
	Namespace string
	Name      string
}

type CACertificate struct {
	organization             string
	gracePeriod              time.Duration
	certificateExpiration    time.Duration
	intermediateCAExpiration time.Duration

	cert *x509.Certificate
	pk   *ecdsa.PrivateKey

	pem           []byte
	privateKeyPEM []byte
}

func (c *CACertificate) Bytes() []byte {
	return c.pem
}

func (c *CACertificate) PrivateKeyBytes() []byte {
	return c.privateKeyPEM
}

func (c *CACertificate) Sign(names ...string) (*Certificate, error) {
	if len(names) == 0 {
		return nil, errors.New("must specify at least one name")
	}

	now := time.Now()
	notBefore := now.Add(-c.gracePeriod)
	notAfter := now.Add(c.certificateExpiration)

	if c.cert.NotBefore.After(notBefore) {
		return nil, errors.New("CA is not valid prior to the certificate not before time, cannot issue a new certificate")
	}

	if c.cert.NotAfter.Before(notAfter) {
		return nil, errors.New("CA expires before certificate expiration time, cannot issue a new certificate")
	}

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating key: %w", err)
	}

	ips := []net.IP{}
	dns := []string{}
	for _, name := range names {
		if ip := net.ParseIP(name); ip != nil {
			ips = append(ips, ip)
		} else {
			dns = append(dns, name)
		}
	}

	serialNumber, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return nil, fmt.Errorf("generating serial number: %w", err)
	}
	tmpl := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   names[0],
			Organization: []string{c.organization},
		},
		NotBefore: notBefore,
		NotAfter:  notAfter,

		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		DNSNames:              dns,
		IPAddresses:           ips,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &tmpl, c.cert, &priv.PublicKey, c.pk)
	if err != nil {
		return nil, fmt.Errorf("creating certificate:: %w", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})

	keyBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("marshaling certificate:: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	return &Certificate{
		privateKey:  keyPEM,
		certificate: certPEM,
	}, nil
}

func (c *CACertificate) Intermediate(name string) (*CACertificate, error) {
	now := time.Now()
	notBefore := now.Add(-c.gracePeriod)
	notAfter := now.Add(c.intermediateCAExpiration)

	if c.cert.NotBefore.After(notBefore) {
		return nil, errors.New("CA is not valid prior to the certificate not before time, cannot issue a new certificate")
	}

	if c.cert.NotAfter.Before(notAfter) {
		return nil, errors.New("CA expires before certificate expiration time, cannot issue a new certificate")
	}

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating key: %w", err)
	}

	serialNumber, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return nil, fmt.Errorf("generating serial number: %w", err)
	}
	tmpl := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   name,
			Organization: []string{c.organization},
		},
		NotBefore: time.Now(),
		NotAfter:  time.Now().Add(365 * 24 * time.Hour),

		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &tmpl, c.cert, &priv.PublicKey, c.pk)
	if err != nil {
		return nil, fmt.Errorf("creating certificate:: %w", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})

	keyBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("marshaling certificate:: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	cert, err := x509.ParseCertificate(derBytes)
	if err != nil {
		return nil, fmt.Errorf("parsing certificate:: %w", err)
	}

	return &CACertificate{
		pk:                       priv,
		cert:                     cert,
		organization:             c.organization,
		certificateExpiration:    c.certificateExpiration,
		intermediateCAExpiration: c.intermediateCAExpiration,
		privateKeyPEM:            keyPEM,
		pem:                      certPEM,
	}, nil
}

type Certificate struct {
	privateKey  []byte
	certificate []byte
}

func (c *Certificate) PrivateKeyBytes() []byte {
	return c.privateKey
}

func (c *Certificate) Bytes() []byte {
	return c.certificate
}

type CAConfiguration struct {
	NotBeforeGracePeriod   time.Duration
	CALifetime             time.Duration
	IntermediateCALifetime time.Duration
	CertificateLifetime    time.Duration
}

func GenerateCA(organization, name string, configuration *CAConfiguration) (*CACertificate, error) {
	if configuration == nil {
		configuration = &CAConfiguration{}
	}
	if configuration.CALifetime == 0 {
		configuration.CALifetime = defaultCAExpiration
	}
	if configuration.IntermediateCALifetime == 0 {
		configuration.IntermediateCALifetime = defaultIntermediateCAExpiration
	}
	if configuration.CertificateLifetime == 0 {
		configuration.CertificateLifetime = defaultCertificateExpiration
	}
	if configuration.NotBeforeGracePeriod == 0 {
		configuration.NotBeforeGracePeriod = notBeforeGracePeriod
	}

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating key: %w", err)
	}

	now := time.Now()
	notBefore := now.Add(-configuration.NotBeforeGracePeriod)
	notAfter := now.Add(configuration.CALifetime)

	serialNumber, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return nil, fmt.Errorf("generating serial number: %w", err)
	}
	caTemplate := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   name,
			Organization: []string{organization},
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &caTemplate, &caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("creating certificate:: %w", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})

	keyBytes, err := x509.MarshalECPrivateKey(caKey)
	if err != nil {
		return nil, fmt.Errorf("marshaling certificate:: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	cert, err := x509.ParseCertificate(derBytes)
	if err != nil {
		return nil, fmt.Errorf("parsing certificate:: %w", err)
	}

	return &CACertificate{
		pk:                       caKey,
		cert:                     cert,
		organization:             organization,
		privateKeyPEM:            keyPEM,
		pem:                      certPEM,
		certificateExpiration:    configuration.CertificateLifetime,
		intermediateCAExpiration: configuration.IntermediateCALifetime,
		gracePeriod:              configuration.NotBeforeGracePeriod,
	}, nil
}

func LoadCA(certPEM, keyPEM []byte, configuration *CAConfiguration) (*CACertificate, error) {
	if configuration == nil {
		configuration = &CAConfiguration{}
	}
	if configuration.CALifetime == 0 {
		configuration.CALifetime = defaultCAExpiration
	}
	if configuration.IntermediateCALifetime == 0 {
		configuration.IntermediateCALifetime = defaultIntermediateCAExpiration
	}
	if configuration.CertificateLifetime == 0 {
		configuration.CertificateLifetime = defaultCertificateExpiration
	}
	if configuration.NotBeforeGracePeriod == 0 {
		configuration.NotBeforeGracePeriod = notBeforeGracePeriod
	}

	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return nil, errors.New("invalid cert PEM block")
	}

	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil, errors.New("invalid key PEM block")
	}

	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing certificate: %w", err)
	}

	pk, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing private key: %w", err)
	}

	if len(cert.Issuer.Organization) == 0 {
		return nil, errors.New("no organzation found in certificate")
	}

	return &CACertificate{
		pk:                       pk,
		cert:                     cert,
		organization:             cert.Issuer.Organization[0],
		privateKeyPEM:            keyPEM,
		pem:                      certPEM,
		certificateExpiration:    configuration.CertificateLifetime,
		intermediateCAExpiration: configuration.IntermediateCALifetime,
		gracePeriod:              configuration.NotBeforeGracePeriod,
	}, nil
}

func CreateTLSSecret(ctx context.Context, ca *CACertificate, certificate *Certificate, configuration *RemoteKubernetesConfiguration) error {
	if configuration == nil {
		configuration = &RemoteKubernetesConfiguration{}
	}
	if configuration.Namespace == "" {
		configuration.Namespace = defaultServiceAccountNamespace
	}
	if configuration.Name == "" {
		configuration.Name = defaultServiceAccountName
	}
	if configuration.RESTConfig == nil {
		if configuration.ContextName == "" {
			return errors.New("either the name of a kubernetes context of a rest Config must be specified")
		}
		config, err := configFromContext(configuration.ContextName)
		if err != nil {
			return fmt.Errorf("getting REST configuration: %v", err)
		}
		configuration.RESTConfig = config
	}

	cl, err := client.New(configuration.RESTConfig, client.Options{})
	if err != nil {
		return fmt.Errorf("initializing client: %v", err)
	}

	if configuration.EnsureNamespace {
		if err := EnsureNamespace(ctx, configuration.Namespace, cl); err != nil {
			return fmt.Errorf("ensuring namespace exists: %v", err)
		}
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configuration.Name + "-certificates",
			Namespace: configuration.Namespace,
		},
		Data: map[string][]byte{
			"ca.crt":  ca.Bytes(),
			"tls.crt": certificate.Bytes(),
			"tls.key": certificate.PrivateKeyBytes(),
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, cl, secret, func() error {
		secret.Data = map[string][]byte{
			"ca.crt":  ca.Bytes(),
			"tls.crt": certificate.Bytes(),
			"tls.key": certificate.PrivateKeyBytes(),
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("creating tls file: %v", err)
	}
	return nil
}
