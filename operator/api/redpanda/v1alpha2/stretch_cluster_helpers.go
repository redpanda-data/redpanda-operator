// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha2

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
)

// NOTE: this file contains helper methods for the StretchClusterSpec and related types.
// They are used in our rendering pipeline for convenience and to encapsulate defaulting and derived values.
// Eventually, we may want to move some of these methods to the main API type definitions when
// we leverage them outside of the StretchCluster-only rendering pipeline, but until then
// they should stay isolated here.

// Byte-size constants used for storage calculations.
const (
	KiB = int64(1024)
	MiB = 1024 * KiB
	GiB = 1024 * MiB
)

// Default listener ports.
const (
	DefaultAdminPort          = int32(9644)
	DefaultKafkaPort          = int32(9093)
	DefaultHTTPPort           = int32(8082)
	DefaultRPCPort            = int32(33145)
	DefaultSchemaRegistryPort = int32(8081)
)

// Default external listener ports.
const (
	DefaultExternalAdminPort          = int32(9645)
	DefaultExternalKafkaPort          = int32(9094)
	DefaultExternalHTTPPort           = int32(8083)
	DefaultExternalSchemaRegistryPort = int32(8084)

	DefaultExternalAdminAdvertisedPort          = int32(31644)
	DefaultExternalKafkaAdvertisedPort          = int32(31092)
	DefaultExternalHTTPAdvertisedPort           = int32(30082)
	DefaultExternalSchemaRegistryAdvertisedPort = int32(30081)
)

// Default image settings.
const (
	DefaultRedpandaRepository      = "docker.redpanda.com/redpandadata/redpanda"
	DefaultSidecarRepository       = "docker.redpanda.com/redpandadata/redpanda-operator"
	DefaultInitContainerRepository = "busybox"
	DefaultInitContainerImageTag   = "latest"
)

// DefaultRedpandaImageTag is the default tag for the Redpanda container image.
// DefaultOperatorImageTag is the default tag for the operator/sidecar container image.
// These are variables so they can be overridden at build time via ldflags or at runtime.
var (
	DefaultRedpandaImageTag = "latest"
	DefaultOperatorImageTag = "latest"
)

// Default cluster settings.
const (
	DefaultClusterDomain               = "cluster.local."
	DefaultSASLMechanism               = "SCRAM-SHA-512"
	DefaultExpectedFS                  = "xfs"
	DefaultRackAwarenessNodeAnnotation = "topology.kubernetes.io/zone"

	// StretchClusterBootstrapUsername is the SCRAM username the operator
	// provisions on a StretchCluster when SASL is enabled. It is added to
	// the superusers list and used by every operator-side client (admin,
	// kafka, schema registry) that needs to authenticate against the
	// cluster.
	StretchClusterBootstrapUsername = "kubernetes-controller"

	// StretchClusterBootstrapPasswordKey is the key under which the
	// generated bootstrap user password lives in the secret returned by
	// [StretchCluster.BootstrapUserSecretName].
	StretchClusterBootstrapPasswordKey = "password"
)

// --- RackAwareness ---

// IsEnabled returns whether rack awareness is enabled. Safe to call on nil receiver.
func (r *RackAwareness) IsEnabled() bool {
	return r != nil && ptr.Deref(r.Enabled, false)
}

// GetNodeAnnotation returns the node annotation key for rack awareness,
// defaulting to "topology.kubernetes.io/zone".
func (r *RackAwareness) GetNodeAnnotation() string {
	if r != nil && r.NodeAnnotation != nil {
		return *r.NodeAnnotation
	}
	return DefaultRackAwarenessNodeAnnotation
}

// --- Monitoring ---

// IsEnabled returns whether monitoring is enabled. Safe to call on nil receiver.
func (m *Monitoring) IsEnabled() bool {
	return m != nil && ptr.Deref(m.Enabled, false)
}

// --- RBAC ---

// IsEnabled returns whether RBAC is enabled (defaults to true). Safe to call on nil receiver.
func (r *RBAC) IsEnabled() bool {
	return r != nil && ptr.Deref(r.Enabled, true)
}

// --- SASL ---

// IsEnabled returns whether SASL is enabled. Safe to call on nil receiver.
func (s *SASL) IsEnabled() bool {
	return s != nil && ptr.Deref(s.Enabled, false)
}

// GetMechanism returns the SASL mechanism, defaulting to SCRAM-SHA-512.
func (s *SASL) GetMechanism() string {
	if s != nil && s.Mechanism != nil && *s.Mechanism != "" {
		return *s.Mechanism
	}
	return DefaultSASLMechanism
}

// --- Auth ---

// IsSASLEnabled returns whether SASL authentication is enabled. Safe to call on nil receiver.
func (a *Auth) IsSASLEnabled() bool {
	return a != nil && a.SASL.IsEnabled()
}

// --- Networking ---

// IsFlatNetwork returns whether cross-cluster mode is set to "flat". Safe to call on nil receiver.
func (n *Networking) IsFlatNetwork() bool {
	return n != nil && ptr.Deref(n.CrossClusterMode, CrossClusterModeMesh) == CrossClusterModeFlat
}

// IsMCS returns whether cross-cluster mode is set to "mcs". Safe to call on nil receiver.
func (n *Networking) IsMCS() bool {
	return n != nil && ptr.Deref(n.CrossClusterMode, CrossClusterModeMesh) == CrossClusterModeMCS
}

// --- TLS ---

// IsEnabled returns whether TLS is globally enabled. Safe to call on nil receiver.
func (t *TLS) IsEnabled() bool {
	return t != nil && ptr.Deref(t.Enabled, false)
}

// --- ListenerTLS ---

// GetCert returns the cert name for a listener, or empty string if unset. Safe to call on nil receiver.
func (l *StretchListenerTLS) GetCert() string {
	if l == nil {
		return ""
	}
	return ptr.Deref(l.Cert, "")
}

// RequiresClientAuth returns whether mTLS is required. Safe to call on nil receiver.
func (l *StretchListenerTLS) RequiresClientAuth() bool {
	return l != nil && ptr.Deref(l.RequireClientAuth, false)
}

// IsTLSEnabled returns whether TLS is enabled for this listener, falling back
// to the global TLS setting if not explicitly configured. Safe to call on nil receiver.
func (l *StretchListenerTLS) IsTLSEnabled(globalTLS *TLS) bool {
	if l != nil && l.Enabled != nil {
		return *l.Enabled
	}
	return globalTLS.IsEnabled()
}

// IsTLSEnabled returns whether TLS is enabled for this API listener, falling back
// to the global TLS setting. Safe to call on nil receiver.
func (l *StretchAPIListener) IsTLSEnabled(globalTLS *TLS) bool {
	if l == nil {
		return globalTLS.IsEnabled()
	}
	return l.TLS.IsTLSEnabled(globalTLS)
}

// IsTLSEnabled returns whether TLS is enabled for the RPC listener, falling back
// to the global TLS setting. Safe to call on nil receiver.
func (r *StretchRPC) IsTLSEnabled(globalTLS *TLS) bool {
	if r == nil {
		return globalTLS.IsEnabled()
	}
	return r.TLS.IsTLSEnabled(globalTLS)
}

// --- Certificate ---

// IsEnabled returns whether this certificate is enabled (defaults to true). Safe to call on nil receiver.
func (c *Certificate) IsEnabled() bool {
	return c != nil && ptr.Deref(c.Enabled, true)
}

// IsCAEnabled returns whether the CA cert is included in trust stores (defaults to true). Safe to call on nil receiver.
func (c *Certificate) IsCAEnabled() bool {
	return c != nil && ptr.Deref(c.CAEnabled, true)
}

// ShouldApplyInternalDNSNames returns whether internal DNS names should be added to certs. Safe to call on nil receiver.
func (c *Certificate) ShouldApplyInternalDNSNames() bool {
	return c != nil && ptr.Deref(c.ApplyInternalDNSNames, false)
}

// --- IssuerRef ---

// GetName returns the issuer name, or empty string if unset. Safe to call on nil receiver.
func (i *IssuerRef) GetName() string {
	if i == nil {
		return ""
	}
	return ptr.Deref(i.Name, "")
}

// GetKind returns the issuer kind, defaulting to "Issuer". Safe to call on nil receiver.
func (i *IssuerRef) GetKind() string {
	if i == nil {
		return "Issuer"
	}
	return ptr.Deref(i.Kind, "Issuer")
}

// GetGroup returns the issuer group, defaulting to "cert-manager.io". Safe to call on nil receiver.
func (i *IssuerRef) GetGroup() string {
	if i == nil {
		return "cert-manager.io"
	}
	return ptr.Deref(i.Group, "cert-manager.io")
}

// --- TrustStore ---

const (
	// TrustStoreMountPath is the absolute path at which truststore files
	// are mounted to the redpanda container.
	TrustStoreMountPath = "/etc/truststores"
)

// TrustStoreFilePath returns the absolute path to the trust store file.
func (t *TrustStore) TrustStoreFilePath() string {
	return fmt.Sprintf("%s/%s", TrustStoreMountPath, t.RelativePath())
}

// RelativePath returns the relative path within the truststores mount.
func (t *TrustStore) RelativePath() string {
	if t.ConfigMapKeyRef != nil {
		return fmt.Sprintf("configmaps/%s-%s", t.ConfigMapKeyRef.Name, t.ConfigMapKeyRef.Key)
	}
	return fmt.Sprintf("secrets/%s-%s", t.SecretKeyRef.Name, t.SecretKeyRef.Key)
}

// VolumeProjection returns a VolumeProjection for mounting this trust store.
func (t *TrustStore) VolumeProjection() corev1.VolumeProjection {
	if t.ConfigMapKeyRef != nil {
		return corev1.VolumeProjection{
			ConfigMap: &corev1.ConfigMapProjection{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: t.ConfigMapKeyRef.Name,
				},
				Items: []corev1.KeyToPath{{
					Key:  t.ConfigMapKeyRef.Key,
					Path: t.RelativePath(),
				}},
			},
		}
	}
	return corev1.VolumeProjection{
		Secret: &corev1.SecretProjection{
			LocalObjectReference: corev1.LocalObjectReference{
				Name: t.SecretKeyRef.Name,
			},
			Items: []corev1.KeyToPath{{
				Key:  t.SecretKeyRef.Key,
				Path: t.RelativePath(),
			}},
		},
	}
}

// --- Tuning ---

// IsTuneAioEventsEnabled returns whether AIO event tuning is enabled. Safe to call on nil receiver.
func (t *Tuning) IsTuneAioEventsEnabled() bool {
	return t != nil && ptr.Deref(t.TuneAioEvents, false)
}

// IsTuneAioEventsEnabled returns whether AIO event tuning is enabled. Safe to call on nil receiver.
func (t *StretchTuning) IsTuneAioEventsEnabled() bool {
	return t != nil && ptr.Deref(t.TuneAioEvents, false)
}

// --- External ---

// IsEnabled returns whether external access is enabled. Safe to call on nil receiver.
func (e *External) IsEnabled() bool {
	return e != nil && ptr.Deref(e.Enabled, false)
}

// GetDomain returns the external domain, or empty string if unset. Safe to call on nil receiver.
func (e *External) GetDomain() string {
	if e == nil {
		return ""
	}
	return ptr.Deref(e.Domain, "")
}

// GetType returns the external service type, or empty string if unset. Safe to call on nil receiver.
func (e *External) GetType() string {
	if e == nil {
		return ""
	}
	return ptr.Deref(e.Type, "")
}

// --- ExternalService ---

// IsEnabled returns whether the external Service is enabled (defaults to true). Safe to call on nil receiver.
func (s *ExternalService) IsEnabled() bool {
	return s != nil && ptr.Deref(s.Enabled, true)
}

// --- ExternalDNS ---

// IsEnabled returns whether externalDNS is enabled. Safe to call on nil receiver.
func (d *ExternalDNS) IsEnabled() bool {
	return d != nil && ptr.Deref(d.Enabled, false)
}

// --- Listener ---

// IsEnabled returns whether a listener is enabled (defaults to true). Safe to call on nil receiver.
func (l *StretchListener) IsEnabled() bool {
	return l != nil && ptr.Deref(l.Enabled, true)
}

// GetPort returns the listener port, falling back to the given default. Safe to call on nil receiver.
func (l *StretchListener) GetPort(defaultPort int32) int32 {
	if l != nil && l.Port != nil {
		return *l.Port
	}
	return defaultPort
}

// --- ExternalListener ---

// IsEnabled returns whether an external listener is enabled (defaults to true). Safe to call on nil receiver.
func (l *StretchExternalListener) IsEnabled() bool {
	return l != nil && ptr.Deref(l.Enabled, true)
}

// GetPort returns the external listener port, falling back to the given default. Safe to call on nil receiver.
func (l *StretchExternalListener) GetPort(defaultPort int32) int32 {
	if l != nil && l.Port != nil {
		return *l.Port
	}
	return defaultPort
}

// GetAdvertisedPort returns the first advertised port if set, otherwise the default.
func (l *StretchExternalListener) GetAdvertisedPort(defaultPort int32) int32 {
	if l != nil && len(l.AdvertisedPorts) > 0 {
		return l.AdvertisedPorts[0]
	}
	return defaultPort
}

// --- ServiceAccount ---

// ShouldCreate returns whether a ServiceAccount should be created (defaults to true). Safe to call on nil receiver.
func (s *ServiceAccount) ShouldCreate() bool {
	return s != nil && ptr.Deref(s.Create, true)
}

// GetName returns the ServiceAccount name, or empty string if unset. Safe to call on nil receiver.
func (s *ServiceAccount) GetName() string {
	if s == nil {
		return ""
	}
	return ptr.Deref(s.Name, "")
}

// GetServiceAccountName returns the effective service account name, falling back to fullname.
// Safe to call on nil receiver.
func (s *ServiceAccount) GetServiceAccountName(fullname string) string {
	if s != nil {
		if s.ShouldCreate() && s.GetName() != "" {
			return s.GetName()
		}
		if s.ShouldCreate() {
			return fullname
		}
		if s.GetName() != "" {
			return s.GetName()
		}
	}
	return fullname
}

// --- PersistentVolume ---

// IsEnabled returns whether persistent volume is enabled. Safe to call on nil receiver.
func (p *PersistentVolume) IsEnabled() bool {
	return p != nil && ptr.Deref(p.Enabled, false)
}

// --- RedpandaImage ---

// GetRepository returns the image repository, defaulting to the official Redpanda repository.
func (i *RedpandaImage) GetRepository() string {
	if i != nil && i.Repository != nil {
		return *i.Repository
	}
	return DefaultRedpandaRepository
}

// GetSidecarRepository returns the image repository, defaulting to the official Redpanda repository.
func (i *RedpandaImage) GetSidecarRepository() string {
	if i != nil && i.Repository != nil {
		return *i.Repository
	}
	return DefaultSidecarRepository
}

// GetTag returns the Redpanda image tag, defaulting to DefaultRedpandaImageTag.
func (i *RedpandaImage) GetTag() string {
	if i != nil && i.Tag != nil {
		return *i.Tag
	}
	return DefaultRedpandaImageTag
}

// AtLeast returns true if the image tag represents a version >= the given version.
// Returns true for unparseable tags (e.g. "latest").
func (i *RedpandaImage) AtLeast(version string) bool {
	tag := strings.TrimPrefix(i.GetTag(), "v")
	version = strings.TrimPrefix(version, "v")

	tagParts := strings.SplitN(tag, ".", 3)
	versionParts := strings.SplitN(version, ".", 3)

	if len(tagParts) < 3 || len(versionParts) < 3 {
		return true
	}

	for i := 0; i < 3; i++ {
		tagParts[i] = strings.SplitN(tagParts[i], "-", 2)[0]
		versionParts[i] = strings.SplitN(versionParts[i], "-", 2)[0]

		if tagParts[i] != versionParts[i] {
			return tagParts[i] > versionParts[i]
		}
	}
	return true
}

// --- FsValidator ---

// IsEnabled returns whether the FS validator init container is enabled. Safe to call on nil receiver.
func (f *FsValidator) IsEnabled() bool {
	return f != nil && ptr.Deref(f.Enabled, false)
}

// GetExpectedFS returns the expected filesystem, defaulting to "xfs".
func (f *FsValidator) GetExpectedFS() string {
	if f != nil && f.ExpectedFS != nil {
		return *f.ExpectedFS
	}
	return DefaultExpectedFS
}

// --- SetDataDirOwnership ---

// IsEnabled returns whether the set-datadir-ownership init container is enabled. Safe to call on nil receiver.
func (s *SetDataDirOwnership) IsEnabled() bool {
	return s != nil && ptr.Deref(s.Enabled, false)
}

// --- PoolFSValidator ---

// IsEnabled returns whether the FS validator init container is enabled. Safe to call on nil receiver.
func (f *PoolFSValidator) IsEnabled() bool {
	return f != nil && ptr.Deref(f.Enabled, false)
}

// GetExpectedFS returns the expected filesystem, defaulting to "xfs".
func (f *PoolFSValidator) GetExpectedFS() string {
	if f != nil && f.ExpectedFS != nil {
		return *f.ExpectedFS
	}
	return DefaultExpectedFS
}

// --- PoolSetDataDirOwnership ---

// IsEnabled returns whether the set-datadir-ownership init container is enabled. Safe to call on nil receiver.
func (s *PoolSetDataDirOwnership) IsEnabled() bool {
	return s != nil && ptr.Deref(s.Enabled, false)
}

// --- Listeners (parameterized helpers) ---

// listenerEntry holds the TLS config and external listeners for a single listener type.
type listenerEntry struct {
	tls       *StretchListenerTLS
	externals map[string]*StretchExternalListener
}

// allListenerEntries returns each configured listener's TLS + externals in a stable order.
// API listeners contribute both internal TLS and external listeners; RPC has no externals.
func (l *StretchListeners) allListenerEntries() []listenerEntry {
	if l == nil {
		return nil
	}
	var entries []listenerEntry
	addAPI := func(api *StretchAPIListener) {
		if api != nil {
			entries = append(entries, listenerEntry{tls: api.TLS, externals: api.External})
		}
	}
	addAPI(l.Admin)
	addAPI(l.Kafka)
	addAPI(l.HTTP)
	addAPI(l.SchemaRegistry)
	if l.RPC != nil {
		entries = append(entries, listenerEntry{tls: l.RPC.TLS})
	}
	return entries
}

// CertRequiresClientAuth returns whether any listener using the given cert name requires mTLS.
// Safe to call on nil receiver.
func (l *StretchListeners) CertRequiresClientAuth(certName string) bool {
	for _, entry := range l.allListenerEntries() {
		if entry.tls.GetCert() == certName && entry.tls.RequiresClientAuth() {
			return true
		}
	}
	return false
}

// CollectCerts returns cert names from all listeners matching pred, deduplicated and ordered.
// Safe to call on nil receiver.
func (l *StretchListeners) CollectCerts(pred func(*StretchListenerTLS) bool) []string {
	seen := map[string]bool{}
	var names []string
	add := func(tls *StretchListenerTLS) {
		name := tls.GetCert()
		if name == "" || seen[name] || !pred(tls) {
			return
		}
		seen[name] = true
		names = append(names, name)
	}
	for _, entry := range l.allListenerEntries() {
		add(entry.tls)
		for _, k := range sortedKeys(entry.externals) {
			if ext := entry.externals[k]; ext != nil {
				add(ext.TLS)
			}
		}
	}
	return names
}

// AdminCertName returns the cert name for the admin listener. Safe to call on nil receiver.
func (l *StretchListeners) AdminCertName() string {
	if l != nil && l.Admin != nil {
		return l.Admin.TLS.GetCert()
	}
	return ""
}

// KafkaCertName returns the cert name for the kafka listener. Safe to call on nil receiver.
func (l *StretchListeners) KafkaCertName() string {
	if l != nil && l.Kafka != nil {
		return l.Kafka.TLS.GetCert()
	}
	return ""
}

// --- Listeners TrustStore helpers ---

// TrustStores collects all TrustStore references from all listeners (internal + external).
// Safe to call on nil receiver.
func (l *StretchListeners) TrustStores(globalTLS *TLS) []*TrustStore {
	var tss []*TrustStore
	for _, entry := range l.allListenerEntries() {
		if entry.tls != nil && entry.tls.IsTLSEnabled(globalTLS) && entry.tls.TrustStore != nil {
			tss = append(tss, entry.tls.TrustStore)
		}
		for _, key := range sortedKeys(entry.externals) {
			ext := entry.externals[key]
			if ext != nil && ext.IsEnabled() && ext.TLS != nil && ext.TLS.IsTLSEnabled(globalTLS) && ext.TLS.TrustStore != nil {
				tss = append(tss, ext.TLS.TrustStore)
			}
		}
	}
	return tss
}

// TrustStoreVolume returns a projected Volume containing all configured truststores,
// or nil if no truststores are configured. Safe to call on nil receiver.
func (l *StretchListeners) TrustStoreVolume(tls *TLS) *corev1.Volume {
	trustStores := l.TrustStores(tls)
	if len(trustStores) == 0 {
		return nil
	}

	cmSources := map[string][]corev1.KeyToPath{}
	secretSources := map[string][]corev1.KeyToPath{}

	for _, ts := range trustStores {
		projection := ts.VolumeProjection()
		if projection.Secret != nil {
			secretSources[projection.Secret.Name] = append(secretSources[projection.Secret.Name], projection.Secret.Items...)
		} else if projection.ConfigMap != nil {
			cmSources[projection.ConfigMap.Name] = append(cmSources[projection.ConfigMap.Name], projection.ConfigMap.Items...)
		}
	}

	var sources []corev1.VolumeProjection

	for _, name := range sortedKeys(cmSources) {
		sources = append(sources, corev1.VolumeProjection{
			ConfigMap: &corev1.ConfigMapProjection{
				LocalObjectReference: corev1.LocalObjectReference{Name: name},
				Items:                dedupKeyToPaths(cmSources[name]),
			},
		})
	}
	for _, name := range sortedKeys(secretSources) {
		sources = append(sources, corev1.VolumeProjection{
			Secret: &corev1.SecretProjection{
				LocalObjectReference: corev1.LocalObjectReference{Name: name},
				Items:                dedupKeyToPaths(secretSources[name]),
			},
		})
	}

	if len(sources) == 0 {
		return nil
	}

	return &corev1.Volume{
		Name: "truststores",
		VolumeSource: corev1.VolumeSource{
			Projected: &corev1.ProjectedVolumeSource{
				Sources: sources,
			},
		},
	}
}

// sortedKeys returns sorted keys of a map.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// dedupKeyToPaths deduplicates KeyToPath entries by key.
func dedupKeyToPaths(items []corev1.KeyToPath) []corev1.KeyToPath {
	seen := map[string]bool{}
	var deduped []corev1.KeyToPath
	for _, item := range items {
		if seen[item.Key] {
			continue
		}
		deduped = append(deduped, item)
		seen[item.Key] = true
	}
	return deduped
}

// --- ListenerTLS truststore helpers ---

// ServerCAPath returns the path to the CA/truststore file for this listener.
// If a TrustStore is configured, its path takes precedence.
// Otherwise falls back to the cert-based path from TLS.CertServerCAPath.
// Safe to call on nil receiver.
func (l *StretchListenerTLS) ServerCAPath(tls *TLS) string {
	if l != nil && l.TrustStore != nil {
		return l.TrustStore.TrustStoreFilePath()
	}
	certName := l.GetCert()
	if certName == "" {
		return ""
	}
	return tls.CertServerCAPath(certName)
}

// --- TLS (parameterized helpers) ---

// CertServerSecretName returns the server cert secret name for the given cert,
// using fullname as the release prefix. Safe to call on nil receiver.
func (t *TLS) CertServerSecretName(fullname, certName string) string {
	if t != nil {
		if cert, ok := t.Certs[certName]; ok {
			if cert.SecretRef != nil && cert.SecretRef.Name != nil {
				return *cert.SecretRef.Name
			}
		}
	}
	return fmt.Sprintf("%s-%s-cert", fullname, certName)
}

// CertClientSecretName returns the client cert secret name for the given cert,
// using fullname as the release prefix. Safe to call on nil receiver.
func (t *TLS) CertClientSecretName(fullname, certName string) string {
	if t != nil {
		if cert, ok := t.Certs[certName]; ok {
			if cert.ClientSecretRef != nil && cert.ClientSecretRef.Name != nil {
				return *cert.ClientSecretRef.Name
			}
		}
	}
	return fmt.Sprintf("%s-%s-client-cert", fullname, certName)
}

// CertServerCAPath returns the CA path for the given cert's server mount.
// Safe to call on nil receiver.
func (t *TLS) CertServerCAPath(certName string) string {
	mountPoint := fmt.Sprintf("/etc/tls/certs/%s", certName)
	if t != nil {
		if cert, ok := t.Certs[certName]; ok {
			if cert.IsCAEnabled() {
				return fmt.Sprintf("%s/ca.crt", mountPoint)
			}
		}
	}
	return fmt.Sprintf("%s/tls.crt", mountPoint)
}

// CertificatesFor returns the server cert secret name, cert key, and client cert secret name
// for the given TLS certificate. Safe to call on nil receiver.
//
// The returned certSecret and certKey identify where the root CA public key can
// be read from. The behaviour depends on how the certificate is configured:
//   - Default (operator-managed CA): the root-certificate secret, key "tls.crt".
//   - SecretRef set: the user-provided secret, key "tls.crt".
//   - IssuerRef set (no SecretRef): the leaf cert secret created by cert-manager,
//     key "ca.crt" (cert-manager populates the CA chain there).
func (t *TLS) CertificatesFor(fullname, name string) (certSecret, certKey, clientSecret string) {
	if t != nil {
		if cert, ok := t.Certs[name]; ok && cert.IsEnabled() {
			certSecret = fmt.Sprintf("%s-%s-root-certificate", fullname, name)
			certKey = corev1.TLSCertKey
			if cert.SecretRef != nil && cert.SecretRef.Name != nil {
				certSecret = *cert.SecretRef.Name
			} else if cert.IssuerRef != nil {
				// When using an external issuer, there is no separate
				// root-certificate secret. cert-manager populates the
				// leaf cert's secret with a ca.crt key containing the
				// CA chain.
				certSecret = t.CertServerSecretName(fullname, name)
				certKey = "ca.crt"
			}

			clientSecret = fmt.Sprintf("%s-%s-client-cert", fullname, name)
			if cert.ClientSecretRef != nil && cert.ClientSecretRef.Name != nil {
				clientSecret = *cert.ClientSecretRef.Name
			}

			return certSecret, certKey, clientSecret
		}
	}

	certSecret = fmt.Sprintf("%s-%s-root-certificate", fullname, name)
	clientSecret = fmt.Sprintf("%s-default-client-cert", fullname)

	return certSecret, corev1.TLSCertKey, clientSecret
}

// --- StretchCluster convenience methods ---

// BootstrapUserSecretName returns the per-cluster name of the Secret holding
// the SCRAM password for [StretchClusterBootstrapUsername]. The
// [StretchClusterBootstrapPasswordKey] field on that Secret holds the
// password. Both the operator's runtime clients and the resource renderers
// must agree on this name — see the canonical references at
// operator/multicluster/secrets.go, operator/multicluster/render_state.go,
// operator/multicluster/statefulset_redpanda.go, and
// operator/pkg/client/stretch_cluster.go.
func (sc *StretchCluster) BootstrapUserSecretName() string {
	return fmt.Sprintf("%s-bootstrap-user", sc.Name)
}

// --- StretchClusterSpec convenience methods ---

// GetResourceRequirements returns the Kubernetes resource requirements from the spec.
// Supports both new-style (Limits/Requests) and legacy (CPU.Cores + Memory.Container.Max) modes.
// Safe to call on nil receiver.
func (s *StretchClusterSpec) GetResourceRequirements() corev1.ResourceRequirements {
	if s == nil {
		return corev1.ResourceRequirements{}
	}
	return resourceRequirements(s.Resources)
}

// resourceRequirements is the implementation shared by
// [StretchClusterSpec.GetResourceRequirements] and
// [BrokerPoolSpec.GetResourceRequirements].
func resourceRequirements(r *StretchResources) corev1.ResourceRequirements {
	if r == nil {
		return corev1.ResourceRequirements{}
	}

	// New-style: explicit Limits/Requests.
	if len(r.Limits) > 0 || len(r.Requests) > 0 {
		return corev1.ResourceRequirements{
			Limits:   r.Limits,
			Requests: r.Requests,
		}
	}

	// Legacy: derive limits from CPU.Cores and Memory.Container.Max.
	limits := corev1.ResourceList{}
	if r.CPU != nil && r.CPU.Cores != nil {
		limits[corev1.ResourceCPU] = *r.CPU.Cores
	}
	if r.Memory != nil && r.Memory.Container != nil && r.Memory.Container.Max != nil {
		limits[corev1.ResourceMemory] = *r.Memory.Container.Max
	}
	if len(limits) == 0 {
		return corev1.ResourceRequirements{}
	}
	return corev1.ResourceRequirements{Limits: limits}
}

// --- Tiered Storage helpers ---

const (
	// DefaultTieredStorageCacheDir is the default cloud storage cache directory.
	DefaultTieredStorageCacheDir = "/var/lib/redpanda/data/cloud_storage_cache"

	// tieredStorageDirVolumeName is the default volume name for tiered storage.
	tieredStorageDirVolumeName = "tiered-storage-dir"
)

// IsTieredStorageEnabled returns whether tiered storage is enabled.
// Safe to call on nil receiver.
func (s *StretchClusterSpec) IsTieredStorageEnabled() bool {
	if s == nil {
		return false
	}
	return isTieredStorageEnabled(s.Storage)
}

// TieredMountType returns the tiered storage mount type. Defaults to "none" if not set.
// Valid values: "none", "hostPath", "emptyDir", "persistentVolume".
func (s *StretchClusterSpec) TieredMountType() string {
	if s == nil {
		return tieredMountType(nil)
	}
	return tieredMountType(s.Storage)
}

// TieredCacheDirectory returns the cloud storage cache directory path.
func (s *StretchClusterSpec) TieredCacheDirectory() string {
	if s == nil {
		return tieredCacheDirectory(nil)
	}
	return tieredCacheDirectory(s.Storage)
}

// TieredStorageVolumeName returns the volume name for tiered storage,
// using NameOverwrite from the PersistentVolume if set.
func (s *StretchClusterSpec) TieredStorageVolumeName() string {
	if s == nil {
		return tieredStorageVolumeName(nil)
	}
	return tieredStorageVolumeName(s.Storage)
}

// TieredStorageHostPath returns the host path for tiered storage.
func (s *StretchClusterSpec) TieredStorageHostPath() string {
	if s == nil {
		return tieredStorageHostPath(nil)
	}
	return tieredStorageHostPath(s.Storage)
}

// --- Storage helpers shared by StretchClusterSpec and BrokerPoolSpec ---

func isTieredStorageEnabled(storage *StretchStorage) bool {
	if storage == nil || storage.Tiered == nil || storage.Tiered.Config == nil {
		return false
	}
	tc := storage.Tiered.Config
	return tc.CloudStorageEnabled != nil && *tc.CloudStorageEnabled
}

func tieredMountType(storage *StretchStorage) string {
	if storage != nil && storage.Tiered != nil && storage.Tiered.MountType != nil {
		return *storage.Tiered.MountType
	}
	return "none"
}

func tieredCacheDirectory(storage *StretchStorage) string {
	if storage != nil && storage.Tiered != nil && storage.Tiered.Config != nil &&
		storage.Tiered.Config.CloudStorageCacheDirectory != nil {
		return *storage.Tiered.Config.CloudStorageCacheDirectory
	}
	return DefaultTieredStorageCacheDir
}

func tieredStorageVolumeName(storage *StretchStorage) string {
	if storage != nil && storage.Tiered != nil &&
		storage.Tiered.PersistentVolume != nil &&
		storage.Tiered.PersistentVolume.NameOverwrite != nil &&
		*storage.Tiered.PersistentVolume.NameOverwrite != "" {
		return *storage.Tiered.PersistentVolume.NameOverwrite
	}
	return tieredStorageDirVolumeName
}

func tieredStorageHostPath(storage *StretchStorage) string {
	if storage != nil && storage.Tiered != nil && storage.Tiered.HostPath != nil {
		return *storage.Tiered.HostPath
	}
	return ""
}

func storageMinFreeBytes(storage *StretchStorage) int64 {
	if storage == nil || storage.PersistentVolume == nil ||
		!storage.PersistentVolume.IsEnabled() || storage.PersistentVolume.Size == nil {
		return 5 * GiB
	}
	fivePercent := storage.PersistentVolume.Size.Value() * 5 / 100
	if fivePercent < 5*GiB {
		return fivePercent
	}
	return 5 * GiB
}

func tieredStorageCacheSize(storage *StretchStorage) *resource.Quantity {
	if storage == nil || storage.Tiered == nil ||
		storage.Tiered.Config == nil || storage.Tiered.Config.CloudStorageCacheSize == nil {
		return nil
	}
	q, err := resource.ParseQuantity(*storage.Tiered.Config.CloudStorageCacheSize)
	if err != nil {
		return nil
	}
	return &q
}

// --- Config access methods ---

// unmarshalRawConfig unmarshals a RawExtension into a map, or returns nil if nil/unparseable.
func unmarshalRawConfig(raw *runtime.RawExtension) map[string]any {
	if raw == nil || raw.Raw == nil {
		return nil
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw.Raw, &cfg); err != nil {
		return nil
	}
	return cfg
}

// GetNodeConfig returns the node config as a map, or nil if not set or unparseable.
// Safe to call on nil receiver.
func (s *StretchClusterSpec) GetNodeConfig() map[string]any {
	if s == nil || s.Config == nil {
		return nil
	}
	return unmarshalRawConfig(s.Config.Node)
}

// GetClusterConfig returns the cluster config as a map, or nil if not set or unparseable.
// Safe to call on nil receiver.
func (s *StretchClusterSpec) GetClusterConfig() map[string]any {
	if s == nil || s.Config == nil {
		return nil
	}
	return unmarshalRawConfig(s.Config.Cluster)
}

// GetTunableConfig returns the tunable config as a map, or nil if not set or unparseable.
// Safe to call on nil receiver.
func (s *StretchClusterSpec) GetTunableConfig() map[string]any {
	if s == nil || s.Config == nil {
		return nil
	}
	return unmarshalRawConfig(s.Config.Tunable)
}

// NodeConfigBoolValue returns the boolean value of a key in the node config.
// Handles both bool and string representations of true/false.
// Safe to call on nil receiver.
func (s *StretchClusterSpec) NodeConfigBoolValue(key string) bool {
	nodeCfg := s.GetNodeConfig()
	if nodeCfg == nil {
		return false
	}
	val, ok := nodeCfg[key]
	if !ok {
		return false
	}
	switch v := val.(type) {
	case bool:
		return v
	case string:
		return v == "true" || v == "True" || v == "TRUE" || v == "1"
	}
	return false
}

// --- Derived config checks ---

// IsAuditLoggingEnabled returns whether audit logging is enabled.
// Requires Redpanda >= 23.3.0, audit logging enabled, and SASL enabled.
// Safe to call on nil receiver.
func (s *StretchClusterSpec) IsAuditLoggingEnabled() bool {
	return s != nil &&
		s.Image.AtLeast("23.3.0") &&
		s.AuditLogging != nil &&
		ptr.Deref(s.AuditLogging.Enabled, false) &&
		s.Auth.IsSASLEnabled()
}

// IsMetricsReporterEnabled returns whether the metrics reporter is enabled.
// Checks both the cluster config enable_metrics_reporter flag and the
// UsageStats.Enabled flag. Defaults to true.
// Safe to call on nil receiver.
func (s *StretchClusterSpec) IsMetricsReporterEnabled() bool {
	if s == nil {
		return true
	}
	if clusterCfg := s.GetClusterConfig(); clusterCfg != nil {
		if val, ok := clusterCfg["enable_metrics_reporter"]; ok {
			if b, ok := val.(bool); ok && !b {
				return false
			}
		}
	}
	if s.Logging != nil && s.Logging.UsageStats != nil {
		if !ptr.Deref(s.Logging.UsageStats.Enabled, true) {
			return false
		}
	}
	return true
}

// GetStorageMinFreeBytes computes storage_min_free_bytes as min(5GiB, 5% of PV size).
// Returns 5GiB if PV is disabled or has no size set.
// Safe to call on nil receiver.
func (s *StretchClusterSpec) GetStorageMinFreeBytes() int64 {
	if s == nil {
		return storageMinFreeBytes(nil)
	}
	return storageMinFreeBytes(s.Storage)
}

// GetTieredStorageCacheSize returns the parsed cloud storage cache size quantity,
// or nil if not set or unparseable.
// Safe to call on nil receiver.
func (s *StretchClusterSpec) GetTieredStorageCacheSize() *resource.Quantity {
	if s == nil {
		return tieredStorageCacheSize(nil)
	}
	return tieredStorageCacheSize(s.Storage)
}

// --- BrokerPool helpers ---

// GetReplicas returns the replica count for a node pool, defaulting to 1.
func (n *RedpandaBrokerPool) GetReplicas() int32 {
	return ptr.Deref(n.Spec.Replicas, 1)
}

// Suffix returns the suffix for this pool's resource names.
// Returns "-<name>" if the pool has a name, or "" otherwise.
func (n *RedpandaBrokerPool) Suffix() string {
	if n.Name != "" {
		return fmt.Sprintf("-%s", n.Name)
	}
	return ""
}

// imageRef builds "repository:tag" from optional repo/tag pointers, using defaults for nil fields.
func imageRef(repo *string, defaultRepo string, tag *string, defaultTag string) string {
	return fmt.Sprintf("%s:%s", ptr.Deref(repo, defaultRepo), ptr.Deref(tag, defaultTag))
}

// RedpandaImage returns the full image reference (repository:tag).
func (n *RedpandaBrokerPool) RedpandaImage() string {
	if n.Spec.Image != nil {
		return imageRef(n.Spec.Image.Repository, DefaultRedpandaRepository, n.Spec.Image.Tag, DefaultRedpandaImageTag)
	}
	return imageRef(nil, DefaultRedpandaRepository, nil, DefaultRedpandaImageTag)
}

// SidecarImage returns the full image reference for the sidecar (repository:tag).
func (n *RedpandaBrokerPool) SidecarImage() string {
	if n.Spec.SidecarImage != nil {
		return imageRef(n.Spec.SidecarImage.Repository, DefaultSidecarRepository, n.Spec.SidecarImage.Tag, DefaultOperatorImageTag)
	}
	return imageRef(nil, DefaultSidecarRepository, nil, DefaultOperatorImageTag)
}

// InitImage returns the full image reference for the init container (repository:tag).
func (n *RedpandaBrokerPool) InitImage() string {
	if n.Spec.InitContainerImage != nil {
		return imageRef(n.Spec.InitContainerImage.Repository, DefaultInitContainerRepository, n.Spec.InitContainerImage.Tag, DefaultInitContainerImageTag)
	}
	return imageRef(nil, DefaultInitContainerRepository, nil, DefaultInitContainerImageTag)
}

// listeners returns a non-nil StretchListeners, defaulting to an empty value.
func (n *BrokerPoolSpec) listeners() StretchListeners {
	if n.Listeners == nil {
		return StretchListeners{}
	}
	return *n.Listeners
}

// IsAdminTLSEnabled returns whether TLS is enabled on the admin listener. Safe to call on nil receiver.
func (n *BrokerPoolSpec) IsAdminTLSEnabled() bool {
	if n == nil {
		return false
	}
	return n.listeners().Admin.IsTLSEnabled(n.TLS)
}

// IsKafkaTLSEnabled returns whether TLS is enabled on the Kafka listener. Safe to call on nil receiver.
func (n *BrokerPoolSpec) IsKafkaTLSEnabled() bool {
	if n == nil {
		return false
	}
	return n.listeners().Kafka.IsTLSEnabled(n.TLS)
}

// IsHTTPTLSEnabled returns whether TLS is enabled on the HTTP listener. Safe to call on nil receiver.
func (n *BrokerPoolSpec) IsHTTPTLSEnabled() bool {
	if n == nil {
		return false
	}
	return n.listeners().HTTP.IsTLSEnabled(n.TLS)
}

// IsSchemaRegistryTLSEnabled returns whether TLS is enabled on the Schema Registry listener. Safe to call on nil receiver.
func (n *BrokerPoolSpec) IsSchemaRegistryTLSEnabled() bool {
	if n == nil {
		return false
	}
	return n.listeners().SchemaRegistry.IsTLSEnabled(n.TLS)
}

// IsRPCTLSEnabled returns whether TLS is enabled on the RPC listener. Safe to call on nil receiver.
func (n *BrokerPoolSpec) IsRPCTLSEnabled() bool {
	if n == nil {
		return false
	}
	return n.listeners().RPC.IsTLSEnabled(n.TLS)
}

// GetClusterDomain returns the cluster domain for this pool, defaulting to "cluster.local". Safe to call on nil receiver.
func (n *BrokerPoolSpec) GetClusterDomain() string {
	if n != nil && n.ClusterDomain != nil {
		return *n.ClusterDomain
	}
	return DefaultClusterDomain
}

// InternalDomain returns the fully qualified internal DNS domain for the headless service,
// using this pool's ClusterDomain. The headless Service itself is cluster-wide (one per
// StretchCluster) so the service-name segment is the cluster fullname. Safe to call on nil receiver.
func (n *BrokerPoolSpec) InternalDomain(fullname, namespace string) string {
	return fmt.Sprintf("%s.%s.svc.%s", fullname, namespace, n.GetClusterDomain())
}

// GetServiceAccountName returns the effective service account name for this pool's resources.
// Safe to call on nil receiver.
func (n *BrokerPoolSpec) GetServiceAccountName(fullname string) string {
	if n == nil {
		return fullname
	}
	return n.ServiceAccount.GetServiceAccountName(fullname)
}

// InUseServerCerts returns the cert names for all listeners with TLS enabled on this pool.
// Safe to call on nil receiver.
func (n *BrokerPoolSpec) InUseServerCerts() []string {
	if n == nil || !n.TLS.IsEnabled() {
		return nil
	}
	return n.Listeners.CollectCerts(func(*StretchListenerTLS) bool { return true })
}

// InUseClientCerts returns the cert names for this pool's listeners requiring client auth (mTLS).
// Safe to call on nil receiver.
func (n *BrokerPoolSpec) InUseClientCerts() []string {
	if n == nil || !n.TLS.IsEnabled() {
		return nil
	}
	return n.Listeners.CollectCerts(func(tls *StretchListenerTLS) bool {
		return tls.RequiresClientAuth()
	})
}

// AdminPort returns this pool's admin API port. Safe to call on nil receiver.
func (n *BrokerPoolSpec) AdminPort() int32 {
	if n != nil && n.Listeners != nil && n.Listeners.Admin != nil {
		return n.Listeners.Admin.GetPort(DefaultAdminPort)
	}
	return DefaultAdminPort
}

// KafkaPort returns this pool's Kafka API port. Safe to call on nil receiver.
func (n *BrokerPoolSpec) KafkaPort() int32 {
	if n != nil && n.Listeners != nil && n.Listeners.Kafka != nil {
		return n.Listeners.Kafka.GetPort(DefaultKafkaPort)
	}
	return DefaultKafkaPort
}

// HTTPPort returns this pool's HTTP Proxy port. Safe to call on nil receiver.
func (n *BrokerPoolSpec) HTTPPort() int32 {
	if n != nil && n.Listeners != nil && n.Listeners.HTTP != nil {
		return n.Listeners.HTTP.GetPort(DefaultHTTPPort)
	}
	return DefaultHTTPPort
}

// RPCPort returns this pool's RPC port. Safe to call on nil receiver.
func (n *BrokerPoolSpec) RPCPort() int32 {
	if n != nil && n.Listeners != nil && n.Listeners.RPC != nil && n.Listeners.RPC.Port != nil {
		return int32(*n.Listeners.RPC.Port)
	}
	return DefaultRPCPort
}

// SchemaRegistryPort returns this pool's Schema Registry port. Safe to call on nil receiver.
func (n *BrokerPoolSpec) SchemaRegistryPort() int32 {
	if n != nil && n.Listeners != nil && n.Listeners.SchemaRegistry != nil {
		return n.Listeners.SchemaRegistry.GetPort(DefaultSchemaRegistryPort)
	}
	return DefaultSchemaRegistryPort
}

// AdminInternalHTTPProtocol returns "https" if admin TLS is enabled on this pool, "http" otherwise.
// Safe to call on nil receiver.
func (n *BrokerPoolSpec) AdminInternalHTTPProtocol() string {
	if n.IsAdminTLSEnabled() {
		return "https"
	}
	return "http"
}

// AdminInternalURL returns the internal admin API URL template for this pool.
// Safe to call on nil receiver.
func (n *BrokerPoolSpec) AdminInternalURL(fullname, namespace string) string {
	return fmt.Sprintf("%s://%s.%s:%d",
		n.AdminInternalHTTPProtocol(),
		"${SERVICE_NAME}",
		strings.TrimSuffix(n.InternalDomain(fullname, namespace), "."),
		n.AdminPort(),
	)
}

// AdminAPIURLs returns the admin API URL for probes for this pool.
// Safe to call on nil receiver.
func (n *BrokerPoolSpec) AdminAPIURLs(fullname, namespace string) string {
	return fmt.Sprintf("${SERVICE_NAME}.%s:%d", n.InternalDomain(fullname, namespace), n.AdminPort())
}

// MergeDefaults populates this pool's nil fields with Helm-equivalent defaults.
// Mirror of StretchClusterSpec.MergeDefaults for fields that now live on the pool.
func (n *BrokerPoolSpec) MergeDefaults() {
	n.mergeDefaultTLS()
	n.mergeDefaultExternal()
	n.mergeDefaultListeners()
	n.mergeDefaultServiceAccount()
	n.mergeDefaultRBAC()
}

// MergeFromCluster fills pool fields by inheriting from the cluster spec for
// the three fields that exist on both: Storage, Resources, ImagePullSecrets.
// The rule is "pool wins, cluster fills":
//   - Storage / Resources: deep field-by-field merge; a non-nil pool subfield
//     wins, otherwise the cluster's subfield is copied in. Map fields
//     (e.g. Resources.Limits, PersistentVolume.Annotations) merge per key —
//     pool's keys override, cluster's other keys are preserved.
//   - ImagePullSecrets: if the pool has any entries, those win as-is; if the
//     pool's slice is empty (or nil), the cluster's slice is inherited.
//
// Called by lifecycle.StretchClusterWithPools.defaultedPoolCopy before
// MergeDefaults, so per-pool defaulting sees the inherited values. Safe on
// a nil cluster argument (no-op for the inheritance step).
func (n *BrokerPoolSpec) MergeFromCluster(cluster *StretchClusterSpec) {
	if n == nil || cluster == nil {
		return
	}
	n.Storage = mergeStorageFrom(n.Storage, cluster.Storage)
	n.Resources = mergeResourcesFrom(n.Resources, cluster.Resources)
	if len(n.ImagePullSecrets) == 0 && len(cluster.ImagePullSecrets) > 0 {
		// Deep-copy so subsequent mutations on the pool don't affect the cluster.
		out := make([]corev1.LocalObjectReference, len(cluster.ImagePullSecrets))
		copy(out, cluster.ImagePullSecrets)
		n.ImagePullSecrets = out
	}
}

// --- BrokerPoolSpec helpers that read Storage / Resources ---
//
// These mirror the StretchClusterSpec helpers. After lifecycle's
// defaultedPoolCopy runs MergeFromCluster, the pool's Storage / Resources
// reflect "pool wins, cluster fills" — so the renderer can read these
// off the pool directly without needing to consult the cluster.

// GetResourceRequirements returns the Kubernetes resource requirements
// derived from this pool's Resources spec. Safe to call on nil receiver.
func (n *BrokerPoolSpec) GetResourceRequirements() corev1.ResourceRequirements {
	if n == nil {
		return corev1.ResourceRequirements{}
	}
	return resourceRequirements(n.Resources)
}

// GetRedpandaStartFlags computes the --memory, --reserve-memory, and --smp
// flags from this pool's Resources spec. Safe to call on nil receiver.
func (n *BrokerPoolSpec) GetRedpandaStartFlags() map[string]string {
	if n == nil {
		return nil
	}
	return redpandaStartFlags(n.Resources)
}

// GetOverProvisionValue returns whether Redpanda should run in
// overprovisioned mode on this pool. Safe to call on nil receiver.
func (n *BrokerPoolSpec) GetOverProvisionValue() bool {
	if n == nil {
		return false
	}
	return overProvisionValue(n.Resources)
}

// GetEnableMemoryLocking returns whether memory locking should be enabled
// on this pool. Safe to call on nil receiver.
func (n *BrokerPoolSpec) GetEnableMemoryLocking() bool {
	if n == nil {
		return false
	}
	return enableMemoryLocking(n.Resources)
}

// IsTieredStorageEnabled returns whether tiered storage is enabled on this
// pool. Safe to call on nil receiver.
func (n *BrokerPoolSpec) IsTieredStorageEnabled() bool {
	if n == nil {
		return false
	}
	return isTieredStorageEnabled(n.Storage)
}

// TieredMountType returns the tiered storage mount type for this pool.
// Defaults to "none". Safe to call on nil receiver.
func (n *BrokerPoolSpec) TieredMountType() string {
	if n == nil {
		return tieredMountType(nil)
	}
	return tieredMountType(n.Storage)
}

// TieredCacheDirectory returns the cloud storage cache directory path for
// this pool. Safe to call on nil receiver.
func (n *BrokerPoolSpec) TieredCacheDirectory() string {
	if n == nil {
		return tieredCacheDirectory(nil)
	}
	return tieredCacheDirectory(n.Storage)
}

// TieredStorageVolumeName returns the volume name for tiered storage on
// this pool. Safe to call on nil receiver.
func (n *BrokerPoolSpec) TieredStorageVolumeName() string {
	if n == nil {
		return tieredStorageVolumeName(nil)
	}
	return tieredStorageVolumeName(n.Storage)
}

// TieredStorageHostPath returns the host path for tiered storage on this
// pool. Safe to call on nil receiver.
func (n *BrokerPoolSpec) TieredStorageHostPath() string {
	if n == nil {
		return tieredStorageHostPath(nil)
	}
	return tieredStorageHostPath(n.Storage)
}

// GetStorageMinFreeBytes computes storage_min_free_bytes for this pool.
// Safe to call on nil receiver.
func (n *BrokerPoolSpec) GetStorageMinFreeBytes() int64 {
	if n == nil {
		return storageMinFreeBytes(nil)
	}
	return storageMinFreeBytes(n.Storage)
}

// GetTieredStorageCacheSize returns the parsed cloud storage cache size
// quantity for this pool, or nil if not set / unparseable. Safe to call
// on nil receiver.
func (n *BrokerPoolSpec) GetTieredStorageCacheSize() *resource.Quantity {
	if n == nil {
		return tieredStorageCacheSize(nil)
	}
	return tieredStorageCacheSize(n.Storage)
}

// mergeStorageFrom returns the pool's Storage with cluster's subfields filling
// any nil sub-pointers. If the pool's Storage is nil it returns a deep copy of
// the cluster's. Either input may be nil.
func mergeStorageFrom(pool, cluster *StretchStorage) *StretchStorage {
	if cluster == nil {
		return pool
	}
	if pool == nil {
		return cluster.DeepCopy()
	}
	if pool.HostPath == nil && cluster.HostPath != nil {
		pool.HostPath = ptr.To(*cluster.HostPath)
	}
	pool.PersistentVolume = mergePersistentVolumeFrom(pool.PersistentVolume, cluster.PersistentVolume)
	pool.Tiered = mergeTieredFrom(pool.Tiered, cluster.Tiered)
	return pool
}

func mergePersistentVolumeFrom(pool, cluster *PersistentVolume) *PersistentVolume {
	if cluster == nil {
		return pool
	}
	if pool == nil {
		return cluster.DeepCopy()
	}
	if pool.Enabled == nil && cluster.Enabled != nil {
		pool.Enabled = ptr.To(*cluster.Enabled)
	}
	if pool.Size == nil && cluster.Size != nil {
		q := cluster.Size.DeepCopy()
		pool.Size = &q
	}
	if pool.StorageClass == nil && cluster.StorageClass != nil {
		pool.StorageClass = ptr.To(*cluster.StorageClass)
	}
	if pool.NameOverwrite == nil && cluster.NameOverwrite != nil {
		pool.NameOverwrite = ptr.To(*cluster.NameOverwrite)
	}
	pool.Annotations = mergeStringMapFrom(pool.Annotations, cluster.Annotations)
	pool.Labels = mergeStringMapFrom(pool.Labels, cluster.Labels)
	return pool
}

func mergeTieredFrom(pool, cluster *StretchTiered) *StretchTiered {
	if cluster == nil {
		return pool
	}
	if pool == nil {
		return cluster.DeepCopy()
	}
	if pool.MountType == nil && cluster.MountType != nil {
		pool.MountType = ptr.To(*cluster.MountType)
	}
	if pool.HostPath == nil && cluster.HostPath != nil {
		pool.HostPath = ptr.To(*cluster.HostPath)
	}
	pool.PersistentVolume = mergePersistentVolumeFrom(pool.PersistentVolume, cluster.PersistentVolume)
	pool.Config = mergeTieredConfigFrom(pool.Config, cluster.Config)
	if pool.CredentialsSecretRef == nil && cluster.CredentialsSecretRef != nil {
		pool.CredentialsSecretRef = cluster.CredentialsSecretRef.DeepCopy()
	}
	return pool
}

func mergeTieredConfigFrom(pool, cluster *StretchTieredConfig) *StretchTieredConfig {
	if cluster == nil {
		return pool
	}
	if pool == nil {
		return cluster.DeepCopy()
	}
	// StretchTieredConfig is a flat struct of scalar pointers; copy field-by-field
	// where pool is nil. Generated DeepCopy on the cluster side is unnecessary here
	// because the scalars are dereferenced into fresh pointers.
	copyBool := func(dst **bool, src *bool) {
		if *dst == nil && src != nil {
			*dst = ptr.To(*src)
		}
	}
	copyStr := func(dst **string, src *string) {
		if *dst == nil && src != nil {
			*dst = ptr.To(*src)
		}
	}
	copyInt := func(dst **int, src *int) {
		if *dst == nil && src != nil {
			*dst = ptr.To(*src)
		}
	}
	copyBool(&pool.CloudStorageEnabled, cluster.CloudStorageEnabled)
	copyStr(&pool.CloudStorageAPIEndpoint, cluster.CloudStorageAPIEndpoint)
	copyInt(&pool.CloudStorageAPIEndpointPort, cluster.CloudStorageAPIEndpointPort)
	copyStr(&pool.CloudStorageBucket, cluster.CloudStorageBucket)
	copyStr(&pool.CloudStorageAzureContainer, cluster.CloudStorageAzureContainer)
	copyStr(&pool.CloudStorageAzureManagedIdentityID, cluster.CloudStorageAzureManagedIdentityID)
	copyStr(&pool.CloudStorageAzureStorageAccount, cluster.CloudStorageAzureStorageAccount)
	copyStr(&pool.CloudStorageAzureSharedKey, cluster.CloudStorageAzureSharedKey)
	copyStr(&pool.CloudStorageAzureADLSEndpoint, cluster.CloudStorageAzureADLSEndpoint)
	copyInt(&pool.CloudStorageAzureADLSPort, cluster.CloudStorageAzureADLSPort)
	copyInt(&pool.CloudStorageCacheCheckInterval, cluster.CloudStorageCacheCheckInterval)
	copyStr(&pool.CloudStorageCacheDirectory, cluster.CloudStorageCacheDirectory)
	copyStr(&pool.CloudStorageCacheSize, cluster.CloudStorageCacheSize)
	copyStr(&pool.CloudStorageCredentialsSource, cluster.CloudStorageCredentialsSource)
	copyBool(&pool.CloudStorageDisableTLS, cluster.CloudStorageDisableTLS)
	copyBool(&pool.CloudStorageEnableRemoteRead, cluster.CloudStorageEnableRemoteRead)
	copyBool(&pool.CloudStorageEnableRemoteWrite, cluster.CloudStorageEnableRemoteWrite)
	copyInt(&pool.CloudStorageInitialBackoffMs, cluster.CloudStorageInitialBackoffMs)
	copyInt(&pool.CloudStorageManifestUploadTimeoutMs, cluster.CloudStorageManifestUploadTimeoutMs)
	copyInt(&pool.CloudStorageMaxConnectionIdleTimeMs, cluster.CloudStorageMaxConnectionIdleTimeMs)
	copyInt(&pool.CloudStorageMaxConnections, cluster.CloudStorageMaxConnections)
	copyStr(&pool.CloudStorageRegion, cluster.CloudStorageRegion)
	copyInt(&pool.CloudStorageSegmentMaxUploadIntervalSec, cluster.CloudStorageSegmentMaxUploadIntervalSec)
	copyInt(&pool.CloudStorageSegmentUploadTimeoutMs, cluster.CloudStorageSegmentUploadTimeoutMs)
	copyStr(&pool.CloudStorageTrustFile, cluster.CloudStorageTrustFile)
	copyInt(&pool.CloudStorageUploadCtrlDCoeff, cluster.CloudStorageUploadCtrlDCoeff)
	copyInt(&pool.CloudStorageUploadCtrlMaxShares, cluster.CloudStorageUploadCtrlMaxShares)
	copyInt(&pool.CloudStorageUploadCtrlMinShares, cluster.CloudStorageUploadCtrlMinShares)
	copyInt(&pool.CloudStorageUploadCtrlPCoeff, cluster.CloudStorageUploadCtrlPCoeff)
	copyInt(&pool.CloudStorageUploadCtrlUpdateIntervalMs, cluster.CloudStorageUploadCtrlUpdateIntervalMs)
	return pool
}

// mergeResourcesFrom merges cluster resource settings into the pool's. Pool's
// non-nil subfields win; cluster fills the rest. Limits / Requests are
// ResourceList (map) — merged per key, pool's keys win.
func mergeResourcesFrom(pool, cluster *StretchResources) *StretchResources {
	if cluster == nil {
		return pool
	}
	if pool == nil {
		return cluster.DeepCopy()
	}
	pool.Limits = mergeResourceListFrom(pool.Limits, cluster.Limits)
	pool.Requests = mergeResourceListFrom(pool.Requests, cluster.Requests)
	pool.CPU = mergeCPUFrom(pool.CPU, cluster.CPU)
	pool.Memory = mergeMemoryFrom(pool.Memory, cluster.Memory)
	return pool
}

func mergeResourceListFrom(pool, cluster corev1.ResourceList) corev1.ResourceList {
	if len(cluster) == 0 {
		return pool
	}
	if pool == nil {
		pool = corev1.ResourceList{}
	}
	for k, v := range cluster {
		if _, ok := pool[k]; !ok {
			pool[k] = v.DeepCopy()
		}
	}
	return pool
}

func mergeCPUFrom(pool, cluster *CPU) *CPU {
	if cluster == nil {
		return pool
	}
	if pool == nil {
		return cluster.DeepCopy()
	}
	if pool.Cores == nil && cluster.Cores != nil {
		q := cluster.Cores.DeepCopy()
		pool.Cores = &q
	}
	if pool.Overprovisioned == nil && cluster.Overprovisioned != nil {
		pool.Overprovisioned = ptr.To(*cluster.Overprovisioned)
	}
	return pool
}

func mergeMemoryFrom(pool, cluster *Memory) *Memory {
	if cluster == nil {
		return pool
	}
	if pool == nil {
		return cluster.DeepCopy()
	}
	pool.Container = mergeContainerResourcesFrom(pool.Container, cluster.Container)
	if pool.EnableMemoryLocking == nil && cluster.EnableMemoryLocking != nil {
		pool.EnableMemoryLocking = ptr.To(*cluster.EnableMemoryLocking)
	}
	if pool.Redpanda == nil && cluster.Redpanda != nil {
		pool.Redpanda = cluster.Redpanda.DeepCopy()
	}
	return pool
}

func mergeContainerResourcesFrom(pool, cluster *ContainerResources) *ContainerResources {
	if cluster == nil {
		return pool
	}
	if pool == nil {
		return cluster.DeepCopy()
	}
	if pool.Max == nil && cluster.Max != nil {
		q := cluster.Max.DeepCopy()
		pool.Max = &q
	}
	if pool.Min == nil && cluster.Min != nil {
		q := cluster.Min.DeepCopy()
		pool.Min = &q
	}
	return pool
}

// mergeStringMapFrom returns a map where pool's keys win and cluster fills
// the rest. Always returns a fresh map when at least one input has entries,
// to avoid aliasing the cluster's storage.
func mergeStringMapFrom(pool, cluster map[string]string) map[string]string {
	if len(cluster) == 0 {
		return pool
	}
	if pool == nil {
		pool = map[string]string{}
	}
	for k, v := range cluster {
		if _, ok := pool[k]; !ok {
			pool[k] = v
		}
	}
	return pool
}

func (n *BrokerPoolSpec) mergeDefaultServiceAccount() {
	if n.ServiceAccount == nil {
		n.ServiceAccount = &ServiceAccount{
			Create: ptr.To(true),
		}
	}
}

func (n *BrokerPoolSpec) mergeDefaultRBAC() {
	if n.RBAC == nil {
		n.RBAC = &RBAC{
			Enabled: ptr.To(true),
		}
	}
}

func (n *BrokerPoolSpec) mergeDefaultTLS() {
	if n.TLS == nil {
		n.TLS = &TLS{}
	}
	if n.TLS.Enabled == nil {
		n.TLS.Enabled = ptr.To(true)
	}
	if n.TLS.Certs == nil {
		n.TLS.Certs = make(map[string]*Certificate)
	}
	if n.TLS.Certs["default"] == nil {
		n.TLS.Certs["default"] = &Certificate{
			CAEnabled: ptr.To(true),
		}
	}
	if n.TLS.Certs["external"] == nil {
		n.TLS.Certs["external"] = &Certificate{
			CAEnabled: ptr.To(true),
		}
	}
}

func (n *BrokerPoolSpec) mergeDefaultExternal() {
	if n.External == nil {
		n.External = &External{}
	}
	if n.External.Enabled == nil {
		n.External.Enabled = ptr.To(true)
	}
	if n.External.Type == nil {
		n.External.Type = ptr.To("NodePort")
	}
	if n.External.Service == nil {
		n.External.Service = &ExternalService{
			Enabled: ptr.To(true),
		}
	} else if n.External.Service.Enabled == nil {
		n.External.Service.Enabled = ptr.To(true)
	}
}

func (n *BrokerPoolSpec) mergeDefaultListeners() {
	if n.Listeners == nil {
		n.Listeners = &StretchListeners{}
	}
	n.mergeDefaultAdminListener()
	n.mergeDefaultKafkaListener()
	n.mergeDefaultHTTPListener()
	n.mergeDefaultSchemaRegistryListener()
	n.mergeDefaultRPCListener()
}

func (n *BrokerPoolSpec) mergeDefaultAdminListener() {
	mergeDefaultAPIListener(&n.Listeners.Admin, DefaultAdminPort, DefaultExternalAdminPort, DefaultExternalAdminAdvertisedPort)
}

func (n *BrokerPoolSpec) mergeDefaultKafkaListener() {
	mergeDefaultAPIListener(&n.Listeners.Kafka, DefaultKafkaPort, DefaultExternalKafkaPort, DefaultExternalKafkaAdvertisedPort)
}

func (n *BrokerPoolSpec) mergeDefaultHTTPListener() {
	mergeDefaultAPIListener(&n.Listeners.HTTP, DefaultHTTPPort, DefaultExternalHTTPPort, DefaultExternalHTTPAdvertisedPort)
}

func (n *BrokerPoolSpec) mergeDefaultSchemaRegistryListener() {
	mergeDefaultAPIListener(&n.Listeners.SchemaRegistry, DefaultSchemaRegistryPort, DefaultExternalSchemaRegistryPort, DefaultExternalSchemaRegistryAdvertisedPort)
}

func (n *BrokerPoolSpec) mergeDefaultRPCListener() {
	if n.Listeners.RPC == nil {
		n.Listeners.RPC = &StretchRPC{}
	}
	if n.Listeners.RPC.Port == nil {
		n.Listeners.RPC.Port = ptr.To(int(DefaultRPCPort))
	}
	if n.Listeners.RPC.TLS == nil {
		n.Listeners.RPC.TLS = &StretchListenerTLS{
			Cert:              ptr.To("default"),
			RequireClientAuth: ptr.To(false),
		}
	}
}

// --- Defaults merging ---
//
// MergeDefaults fills in Helm-equivalent default values for fields that are nil
// on the StretchClusterSpec. This replaces the defaults that were previously
// provided by the Helm chart's values.yaml during the old rendering path.

// MergeDefaults populates nil fields of the spec with defaults matching the
// Helm chart's values.yaml. It is intended to be called on a deep-copied spec
// so that the original CRD object is not mutated.
func (s *StretchClusterSpec) MergeDefaults() {
	s.mergeDefaultStorage()
	s.mergeDefaultTuning()
	s.mergeDefaultResources()
	s.mergeDefaultLogging()
}

func (s *StretchClusterSpec) mergeDefaultStorage() {
	if s.Storage == nil {
		s.Storage = &StretchStorage{}
	}
	if s.Storage.PersistentVolume == nil {
		s.Storage.PersistentVolume = &PersistentVolume{}
	}
	if s.Storage.PersistentVolume.Enabled == nil {
		s.Storage.PersistentVolume.Enabled = ptr.To(true)
	}
	if s.Storage.PersistentVolume.Size == nil {
		size := resource.MustParse("20Gi")
		s.Storage.PersistentVolume.Size = &size
	}
}

func (s *StretchClusterSpec) mergeDefaultTuning() {
	if s.Tuning == nil {
		s.Tuning = &StretchTuning{}
	}
	if s.Tuning.TuneAioEvents == nil {
		s.Tuning.TuneAioEvents = ptr.To(true)
	}
}

func (s *StretchClusterSpec) mergeDefaultResources() {
	if s.Resources == nil {
		s.Resources = &StretchResources{}
	}
	r := s.Resources
	// Only default legacy fields if Limits/Requests are not set.
	if len(r.Limits) == 0 && len(r.Requests) == 0 {
		if r.CPU == nil {
			r.CPU = &CPU{}
		}
		if r.CPU.Cores == nil {
			cores := resource.MustParse("1")
			r.CPU.Cores = &cores
		}
		if r.Memory == nil {
			r.Memory = &Memory{}
		}
		if r.Memory.Container == nil {
			r.Memory.Container = &ContainerResources{}
		}
		if r.Memory.Container.Max == nil {
			max := resource.MustParse("2.5Gi")
			r.Memory.Container.Max = &max
		}
	}
}

func (s *StretchClusterSpec) mergeDefaultLogging() {
	if s.Logging == nil {
		s.Logging = &StretchLogging{}
	}
	if s.Logging.LogLevel == nil {
		s.Logging.LogLevel = ptr.To("info")
	}
}

// GetRedpandaStartFlags computes the --memory, --reserve-memory, and --smp flags
// from the Resources configuration, mirroring the Helm chart logic.
func (s *StretchClusterSpec) GetRedpandaStartFlags() map[string]string {
	if s == nil {
		return nil
	}
	return redpandaStartFlags(s.Resources)
}

// redpandaStartFlags is the implementation shared by
// [StretchClusterSpec.GetRedpandaStartFlags] and
// [BrokerPoolSpec.GetRedpandaStartFlags].
func redpandaStartFlags(r *StretchResources) map[string]string {
	if r == nil {
		return nil
	}
	flags := map[string]string{}

	if len(r.Limits) > 0 && len(r.Requests) > 0 {
		// New mode: Limits and Requests are set.
		// reserve-memory is always 0.
		flags["--reserve-memory"] = "0M"

		// smp: max(1, floor(cpu_millis/1000))
		cpuReq, ok := r.Requests[corev1.ResourceCPU]
		if !ok {
			cpuReq, ok = r.Limits[corev1.ResourceCPU]
		}
		if ok {
			smp := cpuReq.MilliValue() / 1000
			if smp < 1 {
				smp = 1
			}
			flags["--smp"] = fmt.Sprintf("%d", smp)
		}

		// memory: 90% of container memory
		memReq, ok := r.Requests[corev1.ResourceMemory]
		if !ok {
			memReq, ok = r.Limits[corev1.ResourceMemory]
		}
		if ok {
			memory := int64(float64(memReq.Value())*0.90) / (1024 * 1024)
			flags["--memory"] = fmt.Sprintf("%dM", memory)
		}

		return flags
	}

	// Legacy mode: CPU.Cores and Memory.Container are set.
	if r.CPU != nil && r.CPU.Cores != nil {
		if r.CPU.Cores.MilliValue() < 1000 {
			flags["--smp"] = "1"
		} else {
			flags["--smp"] = fmt.Sprintf("%d", r.CPU.Cores.Value())
		}
	}

	containerMemoryMiB := int64(0)
	if r.Memory != nil && r.Memory.Container != nil {
		if r.Memory.Container.Min != nil {
			containerMemoryMiB = r.Memory.Container.Min.Value() / (1024 * 1024)
		} else if r.Memory.Container.Max != nil {
			containerMemoryMiB = r.Memory.Container.Max.Value() / (1024 * 1024)
		}
	}

	if containerMemoryMiB > 0 {
		if r.Memory.Redpanda != nil && r.Memory.Redpanda.Memory != nil {
			flags["--memory"] = fmt.Sprintf("%dM", r.Memory.Redpanda.Memory.Value()/(1024*1024))
		} else {
			flags["--memory"] = fmt.Sprintf("%dM", int64(float64(containerMemoryMiB)*0.8))
		}

		if r.Memory.Redpanda != nil && r.Memory.Redpanda.ReserveMemory != nil {
			flags["--reserve-memory"] = fmt.Sprintf("%dM", r.Memory.Redpanda.ReserveMemory.Value()/(1024*1024))
		} else {
			flags["--reserve-memory"] = fmt.Sprintf("%dM", int64(float64(containerMemoryMiB)*0.002)+200)
		}
	}

	return flags
}

// GetOverProvisionValue returns whether Redpanda should run in overprovisioned mode.
func (s *StretchClusterSpec) GetOverProvisionValue() bool {
	if s == nil {
		return false
	}
	return overProvisionValue(s.Resources)
}

func overProvisionValue(r *StretchResources) bool {
	if r == nil {
		return false
	}

	if len(r.Limits) > 0 && len(r.Requests) > 0 {
		cpuReq, ok := r.Requests[corev1.ResourceCPU]
		if !ok {
			cpuReq, ok = r.Limits[corev1.ResourceCPU]
		}
		return ok && cpuReq.MilliValue() < 1000
	}

	if r.CPU != nil && r.CPU.Cores != nil && r.CPU.Cores.MilliValue() < 1000 {
		return true
	}
	return r.CPU != nil && ptr.Deref(r.CPU.Overprovisioned, false)
}

// GetEnableMemoryLocking returns whether memory locking should be enabled.
func (s *StretchClusterSpec) GetEnableMemoryLocking() bool {
	if s == nil {
		return false
	}
	return enableMemoryLocking(s.Resources)
}

func enableMemoryLocking(r *StretchResources) bool {
	if r == nil || r.Memory == nil {
		return false
	}
	return ptr.Deref(r.Memory.EnableMemoryLocking, false)
}

// mergeDefaultAPIListener ensures an API listener has default port, TLS, and external listeners.
func mergeDefaultAPIListener(listener **StretchAPIListener, defaultPort, extPort, extAdvertisedPort int32) {
	if *listener == nil {
		*listener = &StretchAPIListener{}
	}
	l := *listener
	if l.Port == nil {
		l.Port = ptr.To(defaultPort)
	}
	if l.TLS == nil {
		l.TLS = &StretchListenerTLS{
			Cert:              ptr.To("default"),
			RequireClientAuth: ptr.To(false),
		}
	}
	mergeDefaultExternalListener(l.External, extPort, extAdvertisedPort, &l.External)
}

func mergeDefaultExternalListener(existing map[string]*StretchExternalListener, port int32, advertisedPort int32, target *map[string]*StretchExternalListener) {
	name := "default"
	certName := "external"
	if existing == nil {
		*target = map[string]*StretchExternalListener{
			name: {
				StretchListener: StretchListener{
					Port: ptr.To(port),
					TLS: &StretchListenerTLS{
						Cert: ptr.To(certName),
					},
				},
				AdvertisedPorts: []int32{advertisedPort},
			},
		}
		return
	}
	if existing[name] == nil {
		existing[name] = &StretchExternalListener{
			StretchListener: StretchListener{
				Port: ptr.To(port),
				TLS: &StretchListenerTLS{
					Cert: ptr.To(certName),
				},
			},
			AdvertisedPorts: []int32{advertisedPort},
		}
	}
}
