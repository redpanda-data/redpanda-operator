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
	return n != nil && ptr.Deref(n.CrossClusterMode, "mesh") == "flat"
}

// IsMCS returns whether cross-cluster mode is set to "mcs". Safe to call on nil receiver.
func (n *Networking) IsMCS() bool {
	return n != nil && ptr.Deref(n.CrossClusterMode, "mesh") == "mcs"
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
func (t *TLS) CertificatesFor(fullname, name string) (certSecret, certKey, clientSecret string) {
	if t != nil {
		if cert, ok := t.Certs[name]; ok && cert.IsEnabled() {
			certSecret = fmt.Sprintf("%s-%s-root-certificate", fullname, name)
			if cert.SecretRef != nil && cert.SecretRef.Name != nil {
				certSecret = *cert.SecretRef.Name
			}

			clientSecret = fmt.Sprintf("%s-%s-client-cert", fullname, name)
			if cert.ClientSecretRef != nil && cert.ClientSecretRef.Name != nil {
				clientSecret = *cert.ClientSecretRef.Name
			}

			return certSecret, corev1.TLSCertKey, clientSecret
		}
	}

	certSecret = fmt.Sprintf("%s-%s-root-certificate", fullname, name)
	clientSecret = fmt.Sprintf("%s-default-client-cert", fullname)

	return certSecret, corev1.TLSCertKey, clientSecret
}

// --- StretchClusterSpec convenience methods ---

// IsAdminTLSEnabled returns whether TLS is enabled on the admin listener. Safe to call on nil receiver.
func (s *StretchClusterSpec) IsAdminTLSEnabled() bool {
	if s == nil {
		return false
	}
	return s.listeners().Admin.IsTLSEnabled(s.TLS)
}

// IsKafkaTLSEnabled returns whether TLS is enabled on the Kafka listener. Safe to call on nil receiver.
func (s *StretchClusterSpec) IsKafkaTLSEnabled() bool {
	if s == nil {
		return false
	}
	return s.listeners().Kafka.IsTLSEnabled(s.TLS)
}

// IsHTTPTLSEnabled returns whether TLS is enabled on the HTTP listener. Safe to call on nil receiver.
func (s *StretchClusterSpec) IsHTTPTLSEnabled() bool {
	if s == nil {
		return false
	}
	return s.listeners().HTTP.IsTLSEnabled(s.TLS)
}

// IsSchemaRegistryTLSEnabled returns whether TLS is enabled on the Schema Registry listener. Safe to call on nil receiver.
func (s *StretchClusterSpec) IsSchemaRegistryTLSEnabled() bool {
	if s == nil {
		return false
	}
	return s.listeners().SchemaRegistry.IsTLSEnabled(s.TLS)
}

// IsRPCTLSEnabled returns whether TLS is enabled on the RPC listener. Safe to call on nil receiver.
func (s *StretchClusterSpec) IsRPCTLSEnabled() bool {
	if s == nil {
		return false
	}
	return s.listeners().RPC.IsTLSEnabled(s.TLS)
}

// listeners returns a non-nil StretchListeners, defaulting to an empty value.
func (s *StretchClusterSpec) listeners() StretchListeners {
	if s.Listeners == nil {
		return StretchListeners{}
	}
	return *s.Listeners
}

// GetClusterDomain returns the cluster domain, defaulting to "cluster.local". Safe to call on nil receiver.
func (s *StretchClusterSpec) GetClusterDomain() string {
	if s != nil && s.ClusterDomain != nil {
		return *s.ClusterDomain
	}
	return DefaultClusterDomain
}

// GetServiceName returns the headless service name, falling back to fullname. Safe to call on nil receiver.
func (s *StretchClusterSpec) GetServiceName(fullname string) string {
	if s != nil && s.Service != nil && s.Service.Name != nil && *s.Service.Name != "" {
		return *s.Service.Name
	}
	return fullname
}

// InternalDomain returns the fully qualified internal DNS domain for the headless service.
// Safe to call on nil receiver.
func (s *StretchClusterSpec) InternalDomain(fullname, namespace string) string {
	return fmt.Sprintf("%s.%s.svc.%s", s.GetServiceName(fullname), namespace, s.GetClusterDomain())
}

// GetServiceAccountName returns the effective service account name for the spec. Safe to call on nil receiver.
func (s *StretchClusterSpec) GetServiceAccountName(fullname string) string {
	if s == nil {
		return fullname
	}
	return s.ServiceAccount.GetServiceAccountName(fullname)
}

// GetResourceRequirements returns the Kubernetes resource requirements from the spec.
// Supports both new-style (Limits/Requests) and legacy (CPU.Cores + Memory.Container.Max) modes.
// Safe to call on nil receiver.
func (s *StretchClusterSpec) GetResourceRequirements() corev1.ResourceRequirements {
	if s == nil || s.Resources == nil {
		return corev1.ResourceRequirements{}
	}
	r := s.Resources

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

// InUseServerCerts returns the cert names for all listeners with TLS enabled.
// Safe to call on nil receiver.
func (s *StretchClusterSpec) InUseServerCerts() []string {
	if s == nil || !s.TLS.IsEnabled() {
		return nil
	}
	return s.Listeners.CollectCerts(func(*StretchListenerTLS) bool { return true })
}

// InUseClientCerts returns the cert names for listeners requiring client auth (mTLS).
// Safe to call on nil receiver.
func (s *StretchClusterSpec) InUseClientCerts() []string {
	if s == nil || !s.TLS.IsEnabled() {
		return nil
	}
	return s.Listeners.CollectCerts(func(tls *StretchListenerTLS) bool {
		return tls.RequiresClientAuth()
	})
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
	if s == nil || s.Storage == nil || s.Storage.Tiered == nil || s.Storage.Tiered.Config == nil {
		return false
	}
	tc := s.Storage.Tiered.Config
	return tc.CloudStorageEnabled != nil && *tc.CloudStorageEnabled
}

// TieredMountType returns the tiered storage mount type. Defaults to "none" if not set.
// Valid values: "none", "hostPath", "emptyDir", "persistentVolume".
func (s *StretchClusterSpec) TieredMountType() string {
	if s != nil && s.Storage != nil && s.Storage.Tiered != nil && s.Storage.Tiered.MountType != nil {
		return *s.Storage.Tiered.MountType
	}
	return "none"
}

// TieredCacheDirectory returns the cloud storage cache directory path.
func (s *StretchClusterSpec) TieredCacheDirectory() string {
	if s != nil && s.Storage != nil && s.Storage.Tiered != nil && s.Storage.Tiered.Config != nil {
		if s.Storage.Tiered.Config.CloudStorageCacheDirectory != nil {
			return *s.Storage.Tiered.Config.CloudStorageCacheDirectory
		}
	}
	return DefaultTieredStorageCacheDir
}

// TieredStorageVolumeName returns the volume name for tiered storage,
// using NameOverwrite from the PersistentVolume if set.
func (s *StretchClusterSpec) TieredStorageVolumeName() string {
	if s != nil && s.Storage != nil && s.Storage.Tiered != nil &&
		s.Storage.Tiered.PersistentVolume != nil &&
		s.Storage.Tiered.PersistentVolume.NameOverwrite != nil &&
		*s.Storage.Tiered.PersistentVolume.NameOverwrite != "" {
		return *s.Storage.Tiered.PersistentVolume.NameOverwrite
	}
	return tieredStorageDirVolumeName
}

// TieredStorageHostPath returns the host path for tiered storage.
func (s *StretchClusterSpec) TieredStorageHostPath() string {
	if s != nil && s.Storage != nil && s.Storage.Tiered != nil && s.Storage.Tiered.HostPath != nil {
		return *s.Storage.Tiered.HostPath
	}
	return ""
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
	if s == nil || s.Storage == nil || s.Storage.PersistentVolume == nil ||
		!s.Storage.PersistentVolume.IsEnabled() || s.Storage.PersistentVolume.Size == nil {
		return 5 * GiB
	}
	fivePercent := s.Storage.PersistentVolume.Size.Value() * 5 / 100
	if fivePercent < 5*GiB {
		return fivePercent
	}
	return 5 * GiB
}

// GetTieredStorageCacheSize returns the parsed cloud storage cache size quantity,
// or nil if not set or unparseable.
// Safe to call on nil receiver.
func (s *StretchClusterSpec) GetTieredStorageCacheSize() *resource.Quantity {
	if s == nil || s.Storage == nil || s.Storage.Tiered == nil ||
		s.Storage.Tiered.Config == nil || s.Storage.Tiered.Config.CloudStorageCacheSize == nil {
		return nil
	}
	q, err := resource.ParseQuantity(*s.Storage.Tiered.Config.CloudStorageCacheSize)
	if err != nil {
		return nil
	}
	return &q
}

// --- NodePool helpers ---

// AdminPort returns the admin API port. Safe to call on nil receiver.
func (s *StretchClusterSpec) AdminPort() int32 {
	if s != nil && s.Listeners != nil && s.Listeners.Admin != nil {
		return s.Listeners.Admin.GetPort(DefaultAdminPort)
	}
	return DefaultAdminPort
}

// KafkaPort returns the Kafka API port. Safe to call on nil receiver.
func (s *StretchClusterSpec) KafkaPort() int32 {
	if s != nil && s.Listeners != nil && s.Listeners.Kafka != nil {
		return s.Listeners.Kafka.GetPort(DefaultKafkaPort)
	}
	return DefaultKafkaPort
}

// HTTPPort returns the HTTP Proxy port. Safe to call on nil receiver.
func (s *StretchClusterSpec) HTTPPort() int32 {
	if s != nil && s.Listeners != nil && s.Listeners.HTTP != nil {
		return s.Listeners.HTTP.GetPort(DefaultHTTPPort)
	}
	return DefaultHTTPPort
}

// RPCPort returns the RPC port. Safe to call on nil receiver.
func (s *StretchClusterSpec) RPCPort() int32 {
	if s != nil && s.Listeners != nil && s.Listeners.RPC != nil && s.Listeners.RPC.Port != nil {
		return int32(*s.Listeners.RPC.Port)
	}
	return DefaultRPCPort
}

// SchemaRegistryPort returns the Schema Registry port. Safe to call on nil receiver.
func (s *StretchClusterSpec) SchemaRegistryPort() int32 {
	if s != nil && s.Listeners != nil && s.Listeners.SchemaRegistry != nil {
		return s.Listeners.SchemaRegistry.GetPort(DefaultSchemaRegistryPort)
	}
	return DefaultSchemaRegistryPort
}

// AdminInternalHTTPProtocol returns "https" if admin TLS is enabled, "http" otherwise.
// Safe to call on nil receiver.
func (s *StretchClusterSpec) AdminInternalHTTPProtocol() string {
	if s.IsAdminTLSEnabled() {
		return "https"
	}
	return "http"
}

// AdminInternalURL returns the internal admin API URL template.
// Safe to call on nil receiver.
func (s *StretchClusterSpec) AdminInternalURL(fullname, namespace string) string {
	return fmt.Sprintf("%s://%s.%s:%d",
		s.AdminInternalHTTPProtocol(),
		"${SERVICE_NAME}",
		strings.TrimSuffix(s.InternalDomain(fullname, namespace), "."),
		s.AdminPort(),
	)
}

// AdminAPIURLs returns the admin API URL for probes.
// Safe to call on nil receiver.
func (s *StretchClusterSpec) AdminAPIURLs(fullname, namespace string) string {
	return fmt.Sprintf("${SERVICE_NAME}.%s:%d", s.InternalDomain(fullname, namespace), s.AdminPort())
}

// GetReplicas returns the replica count for a node pool, defaulting to 1.
func (n *NodePool) GetReplicas() int32 {
	return ptr.Deref(n.Spec.Replicas, 1)
}

// Suffix returns the suffix for this pool's resource names.
// Returns "-<name>" if the pool has a name, or "" otherwise.
func (n *NodePool) Suffix() string {
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
func (n *NodePool) RedpandaImage() string {
	if n.Spec.Image != nil {
		return imageRef(n.Spec.Image.Repository, DefaultRedpandaRepository, n.Spec.Image.Tag, DefaultRedpandaImageTag)
	}
	return imageRef(nil, DefaultRedpandaRepository, nil, DefaultRedpandaImageTag)
}

// SidecarImage returns the full image reference for the sidecar (repository:tag).
func (n *NodePool) SidecarImage() string {
	if n.Spec.SidecarImage != nil {
		return imageRef(n.Spec.SidecarImage.Repository, DefaultSidecarRepository, n.Spec.SidecarImage.Tag, DefaultOperatorImageTag)
	}
	return imageRef(nil, DefaultSidecarRepository, nil, DefaultOperatorImageTag)
}

// InitImage returns the full image reference for the init container (repository:tag).
func (n *NodePool) InitImage() string {
	if n.Spec.InitContainerImage != nil {
		return imageRef(n.Spec.InitContainerImage.Repository, DefaultInitContainerRepository, n.Spec.InitContainerImage.Tag, DefaultInitContainerImageTag)
	}
	return imageRef(nil, DefaultInitContainerRepository, nil, DefaultInitContainerImageTag)
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
	s.mergeDefaultTLS()
	s.mergeDefaultExternal()
	s.mergeDefaultListeners()
	s.mergeDefaultServiceAccount()
	s.mergeDefaultRBAC()
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
	if s == nil || s.Resources == nil {
		return nil
	}
	r := s.Resources
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
	if s == nil || s.Resources == nil {
		return false
	}
	r := s.Resources

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
	if s == nil || s.Resources == nil || s.Resources.Memory == nil {
		return false
	}
	return ptr.Deref(s.Resources.Memory.EnableMemoryLocking, false)
}

func (s *StretchClusterSpec) mergeDefaultServiceAccount() {
	if s.ServiceAccount == nil {
		s.ServiceAccount = &ServiceAccount{
			Create: ptr.To(true),
		}
	}
}

func (s *StretchClusterSpec) mergeDefaultRBAC() {
	if s.RBAC == nil {
		s.RBAC = &RBAC{
			Enabled: ptr.To(true),
		}
	}
}

func (s *StretchClusterSpec) mergeDefaultTLS() {
	if s.TLS == nil {
		s.TLS = &TLS{}
	}
	if s.TLS.Enabled == nil {
		s.TLS.Enabled = ptr.To(true)
	}
	if s.TLS.Certs == nil {
		s.TLS.Certs = make(map[string]*Certificate)
	}
	if s.TLS.Certs["default"] == nil {
		s.TLS.Certs["default"] = &Certificate{
			CAEnabled: ptr.To(true),
		}
	}
	if s.TLS.Certs["external"] == nil {
		s.TLS.Certs["external"] = &Certificate{
			CAEnabled: ptr.To(true),
		}
	}
}

func (s *StretchClusterSpec) mergeDefaultExternal() {
	if s.External == nil {
		s.External = &External{}
	}
	if s.External.Enabled == nil {
		s.External.Enabled = ptr.To(true)
	}
	if s.External.Type == nil {
		s.External.Type = ptr.To("NodePort")
	}
	if s.External.Service == nil {
		s.External.Service = &ExternalService{
			Enabled: ptr.To(true),
		}
	} else if s.External.Service.Enabled == nil {
		s.External.Service.Enabled = ptr.To(true)
	}
}

func (s *StretchClusterSpec) mergeDefaultListeners() {
	if s.Listeners == nil {
		s.Listeners = &StretchListeners{}
	}
	s.mergeDefaultAdminListener()
	s.mergeDefaultKafkaListener()
	s.mergeDefaultHTTPListener()
	s.mergeDefaultSchemaRegistryListener()
	s.mergeDefaultRPCListener()
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

func (s *StretchClusterSpec) mergeDefaultAdminListener() {
	mergeDefaultAPIListener(&s.Listeners.Admin, DefaultAdminPort, DefaultExternalAdminPort, DefaultExternalAdminAdvertisedPort)
}

func (s *StretchClusterSpec) mergeDefaultKafkaListener() {
	mergeDefaultAPIListener(&s.Listeners.Kafka, DefaultKafkaPort, DefaultExternalKafkaPort, DefaultExternalKafkaAdvertisedPort)
}

func (s *StretchClusterSpec) mergeDefaultHTTPListener() {
	mergeDefaultAPIListener(&s.Listeners.HTTP, DefaultHTTPPort, DefaultExternalHTTPPort, DefaultExternalHTTPAdvertisedPort)
}

func (s *StretchClusterSpec) mergeDefaultSchemaRegistryListener() {
	mergeDefaultAPIListener(&s.Listeners.SchemaRegistry, DefaultSchemaRegistryPort, DefaultExternalSchemaRegistryPort, DefaultExternalSchemaRegistryAdvertisedPort)
}

func (s *StretchClusterSpec) mergeDefaultRPCListener() {
	if s.Listeners.RPC == nil {
		s.Listeners.RPC = &StretchRPC{}
	}
	if s.Listeners.RPC.Port == nil {
		s.Listeners.RPC.Port = ptr.To(int(DefaultRPCPort))
	}
	if s.Listeners.RPC.TLS == nil {
		s.Listeners.RPC.TLS = &StretchListenerTLS{
			Cert:              ptr.To("default"),
			RequireClientAuth: ptr.To(false),
		}
	}
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
