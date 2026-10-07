// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package redpanda

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

const (
	// trustStoreMountPath is the mount path of all truststores. Use the
	// [TrustStore] methods to make paths.
	trustStoreMountPath = "/etc/truststores"

	// OSTrustStorePath is the CA bundle of the container image.
	OSTrustStorePath = "/etc/ssl/certs/ca-certificates.crt"

	// ReservedListenerName is the name of [API.Reserved]. Redpanda does not
	// give special behavior to this name.
	ReservedListenerName = "internal"

	trustStoreVolumeName = "truststores"
)

// APIKind identifies a listener set in redpanda.yaml. All redpanda.yaml keys
// of an [API] come from its APIKind.
type APIKind string

const (
	AdminAPI          APIKind = "admin"
	KafkaAPI          APIKind = "kafka"
	HTTPAPI           APIKind = "http"
	SchemaRegistryAPI APIKind = "schema"
	RPCAPI            APIKind = "rpc"
)

// ConfigKey returns the redpanda.yaml key of the listener list.
func (k APIKind) ConfigKey() string {
	if k == AdminAPI {
		return "admin"
	}
	if k == HTTPAPI {
		return "pandaproxy_api"
	}
	if k == SchemaRegistryAPI {
		return "schema_registry_api"
	}
	if k == RPCAPI {
		return "rpc_server"
	}
	if k == KafkaAPI {
		return "kafka_api"
	}
	return ""
}

// TLSConfigKey returns the redpanda.yaml key of the TLS list.
func (k APIKind) TLSConfigKey() string {
	if k == AdminAPI {
		return "admin_api_tls"
	}
	return fmt.Sprintf("%s_tls", k.ConfigKey())
}

// ConfigSection returns the top-level redpanda.yaml key that contains
// [APIKind.ConfigKey].
func (k APIKind) ConfigSection() string {
	if k == HTTPAPI {
		return "pandaproxy"
	}
	if k == SchemaRegistryAPI {
		return "schema_registry"
	}
	return "redpanda"
}

// AdvertisedConfigKey returns the key of the advertised addresses. It is empty
// if this kind has no advertised addresses.
func (k APIKind) AdvertisedConfigKey() string {
	if k != KafkaAPI && k != HTTPAPI {
		return ""
	}
	return fmt.Sprintf("advertised_%s", k.ConfigKey())
}

// AdvertisedConfigSection returns the section that contains
// [APIKind.AdvertisedConfigKey]. It is empty if that key is empty.
func (k APIKind) AdvertisedConfigSection() string {
	if k.AdvertisedConfigKey() == "" {
		return ""
	}
	return k.ConfigSection()
}

// ReservedPortName returns the port name of [API.Reserved].
func (k APIKind) ReservedPortName() string {
	if k == SchemaRegistryAPI {
		return "schemaregistry"
	}
	return string(k)
}

// PortName returns a Service port name for this kind and listener.
func (k APIKind) PortName(listener string) string {
	return fmt.Sprintf("%s-%s", k, listener)
}

// ContainerPortName returns [APIKind.PortName] in a format that a container
// port accepts. The maximum length is 15 characters. If two listeners have the
// same first eight lowercase characters, Kubernetes rejects the duplicate name.
func (k APIKind) ContainerPortName(listener string) string {
	return fmt.Sprintf("%s-%.8s", k, strings.ToLower(listener))
}

// Listeners is the resolved listener set of a cluster. It contains the
// listeners that Redpanda binds, the listeners that Kubernetes exposes, and the
// certificate of each listener.
type Listeners struct {
	// ByKind contains all APIs that the cluster binds. Use the accessor and
	// order methods when possible.
	ByKind map[APIKind]*API
}

func NewListeners(apis []API) Listeners {
	byKind := map[APIKind]*API{}
	for _, api := range apis {
		byKind[api.Kind] = &api
	}
	return Listeners{ByKind: byKind}
}

func (l *Listeners) Admin() *API          { return l.ByKind[AdminAPI] }
func (l *Listeners) Kafka() *API          { return l.ByKind[KafkaAPI] }
func (l *Listeners) HTTP() *API           { return l.ByKind[HTTPAPI] }
func (l *Listeners) SchemaRegistry() *API { return l.ByKind[SchemaRegistryAPI] }
func (l *Listeners) RPC() *API            { return l.ByKind[RPCAPI] }

// InOrder returns the APIs of kinds, in the sequence of kinds. It skips a kind
// that the cluster does not bind.
func (l *Listeners) InOrder(kinds []APIKind) []*API {
	var apis []*API
	for _, kind := range kinds {
		if api, ok := l.ByKind[kind]; ok {
			apis = append(apis, api)
		}
	}
	return apis
}

// ListenersInOrder returns the listeners of the APIs of kinds, in the sequence
// of kinds. See [Listeners.InOrder].
func (l *Listeners) ListenersInOrder(kinds []APIKind) []Listener {
	var listeners []Listener
	for _, api := range l.InOrder(kinds) {
		listeners = append(listeners, api.Listeners()...)
	}
	return listeners
}

// Reserved returns the [API.Reserved] listener of each API. It omits an API
// that has no reserved listener.
func (l *Listeners) Reserved() Listeners {
	var apis []API
	for _, kind := range slices.Sorted(maps.Keys(l.ByKind)) {
		api := l.ByKind[kind]
		if api.Reserved != nil {
			apis = append(apis, API{Kind: kind, Reserved: api.Reserved})
		}
	}
	return NewListeners(apis)
}

// Additional returns the [API.Additional] listeners of each API. It omits an
// API that has no additional listeners.
func (l *Listeners) Additional() Listeners {
	var apis []API
	for _, kind := range slices.Sorted(maps.Keys(l.ByKind)) {
		api := l.ByKind[kind]
		if len(api.Additional) > 0 {
			apis = append(apis, API{Kind: kind, Additional: api.Additional})
		}
	}
	return NewListeners(apis)
}

// ConfigSections renders every API's redpanda.yaml entries, keyed by the top
// level section they nest under.
func (l *Listeners) ConfigSections() map[string]map[string]any {
	// NB: Two APIs can use the same section, but not the same entry key. Thus,
	// no entry overwrites an entry of a different API.
	sections := map[string]map[string]any{
		"redpanda":        {},
		"pandaproxy":      {},
		"schema_registry": {},
	}

	for _, api := range l.InOrder([]APIKind{AdminAPI, KafkaAPI, HTTPAPI, SchemaRegistryAPI}) {
		maps.Copy(sections[api.Kind.ConfigSection()], api.configEntries())
	}
	maps.Copy(sections[RPCAPI.ConfigSection()], l.rpcConfigEntries())

	return sections
}

// rpcConfigEntries renders rpc_server and rpc_server_tls, which take one
// listener as a bare map rather than the named list the *_api keys carry.
func (l *Listeners) rpcConfigEntries() map[string]any {
	listener := l.RPC().Reserved

	entries := map[string]any{
		RPCAPI.ConfigKey(): map[string]any{
			"address": listener.Address,
			"port":    listener.Port,
		},
	}

	if tls := listener.TLS; tls != nil {
		entries[RPCAPI.TLSConfigKey()] = tls.configEntry()
	}

	return entries
}

// ContainerPorts returns the ports of the redpanda container.
func (l *Listeners) ContainerPorts() []corev1.ContainerPort {
	var ports []corev1.ContainerPort

	// NB: The container ports are part of the pod template. Thus, a change to
	// this sequence restarts all brokers.
	for _, api := range l.InOrder([]APIKind{AdminAPI, HTTPAPI, KafkaAPI, RPCAPI, SchemaRegistryAPI}) {
		for _, listener := range api.Listeners() {
			ports = append(ports, corev1.ContainerPort{
				Name:          listener.ContainerPortName,
				ContainerPort: listener.Port,
			})
		}
	}

	return ports
}

// TrustStores returns every active truststore. This includes the RPC
// truststore, because rpc_server_tls has a truststore_file.
func (l *Listeners) TrustStores() []*TrustStore {
	var stores []*TrustStore

	for _, api := range l.InOrder([]APIKind{KafkaAPI, AdminAPI, HTTPAPI, SchemaRegistryAPI, RPCAPI}) {
		for _, listener := range api.Listeners() {
			if listener.TLS == nil {
				continue
			}
			if listener.TLS.TrustStore == nil {
				continue
			}
			stores = append(stores, listener.TLS.TrustStore)
		}
	}

	return stores
}

// TrustStoreVolume returns one Volume that projects all truststores. ConfigMap
// sources come before Secret sources. Each group is sorted by name. It returns
// nil if there are no truststores.
func (l *Listeners) TrustStoreVolume() *corev1.Volume {
	cmSources := map[string][]corev1.KeyToPath{}
	secretSources := map[string][]corev1.KeyToPath{}

	for _, store := range l.TrustStores() {
		projection := store.VolumeProjection()

		if projection.Secret != nil {
			secretSources[projection.Secret.Name] = append(secretSources[projection.Secret.Name], projection.Secret.Items...)
		} else {
			cmSources[projection.ConfigMap.Name] = append(cmSources[projection.ConfigMap.Name], projection.ConfigMap.Items...)
		}
	}

	var sources []corev1.VolumeProjection

	for _, name := range slices.Sorted(maps.Keys(cmSources)) {
		sources = append(sources, corev1.VolumeProjection{
			ConfigMap: &corev1.ConfigMapProjection{
				LocalObjectReference: corev1.LocalObjectReference{Name: name},
				Items:                dedupKeyToPaths(cmSources[name]),
			},
		})
	}

	for _, name := range slices.Sorted(maps.Keys(secretSources)) {
		sources = append(sources, corev1.VolumeProjection{
			Secret: &corev1.SecretProjection{
				LocalObjectReference: corev1.LocalObjectReference{Name: name},
				Items:                dedupKeyToPaths(secretSources[name]),
			},
		})
	}

	if len(sources) < 1 {
		return nil
	}

	return &corev1.Volume{
		Name: trustStoreVolumeName,
		VolumeSource: corev1.VolumeSource{
			Projected: &corev1.ProjectedVolumeSource{Sources: sources},
		},
	}
}

// TrustStoreMount returns the VolumeMount of [Listeners.TrustStoreVolume]. It
// returns nil if there are no truststores.
func (l *Listeners) TrustStoreMount() *corev1.VolumeMount {
	if len(l.TrustStores()) < 1 {
		return nil
	}

	return &corev1.VolumeMount{
		Name:      trustStoreVolumeName,
		MountPath: trustStoreMountPath,
		ReadOnly:  true,
	}
}

// API is a listener set in redpanda.yaml: an *_api key, its *_tls key, and
// its listeners. rpc_server also uses this type.
type API struct {
	// Kind is the prefix of the port and TLSRoute names. All redpanda.yaml
	// keys of this API come from Kind.
	Kind APIKind

	// Reserved is the listener that the chart and the operator use: the probes,
	// the sidecar, rpk, the clients of Redpanda itself, and the headless
	// Service. Its name is [ReservedListenerName]. It is nil if the API binds no
	// reserved listener.
	Reserved *Listener

	// Additional contains the other listeners in redpanda.yaml order.
	Additional []Listener
}

// Listeners returns all listeners of the API in redpanda.yaml order.
func (a *API) Listeners() []Listener {
	var listeners []Listener
	if a.Reserved != nil {
		listeners = append(listeners, *a.Reserved)
	}
	return append(listeners, a.Additional...)
}

// RPKClientTLS returns the rpk TLS configuration of this API. It returns nil,
// not an empty map, if the reserved listener has no TLS.
func (a *API) RPKClientTLS() map[string]any {
	listener := a.Reserved

	tls := listener.TLS
	if tls == nil {
		return nil
	}

	cfg := map[string]any{
		"ca_file": tls.ServerCAFile(),
	}

	if kp := tls.Client; kp != nil {
		cfg["cert_file"] = kp.CertFile()
		cfg["key_file"] = kp.KeyFile()
	}

	return cfg
}

// BrokerClientTLS returns the broker_tls block that the Redpanda clients use
// to connect to this API. It returns nil if the reserved listener has no TLS.
func (a *API) BrokerClientTLS() map[string]any {
	listener := a.Reserved

	tls := listener.TLS
	if tls == nil {
		return nil
	}

	cfg := map[string]any{
		"enabled":             true,
		"require_client_auth": tls.RequireClientAuth,
		// NB: truststore_file is the same as ca_file in the rpk configuration.
		// Redpanda does not read ca_file.
		"truststore_file": tls.ServerCAFile(),
	}

	if kp := tls.Client; kp != nil {
		cfg["cert_file"] = kp.CertFile()
		cfg["key_file"] = kp.KeyFile()
	}

	return cfg
}

// CurlFlags returns the curl TLS flags for the reserved listener of this API.
// It returns an empty string if the listener has no TLS.
func (a *API) CurlFlags() string {
	listener := a.Reserved

	tls := listener.TLS
	if tls == nil {
		return ""
	}

	flags := fmt.Sprintf("--cacert %s", tls.ServerCAFile())
	if kp := tls.Client; kp != nil {
		flags = fmt.Sprintf("%s --cert %s --key %s", flags, kp.CertFile(), kp.KeyFile())
	}

	return flags
}

// ProfileAdvertisedPort returns the port that an rpk profile gives to external
// clients for replica. It can be different from [Listener.AdvertisedPort],
// which the configurator uses.
func (a *API) ProfileAdvertisedPort(replica int32) int32 {
	// NB: The fallback is the port of the reserved listener, not the port of
	// the additional listener.
	port := a.Reserved.Port

	if len(a.Additional) < 1 {
		return port
	}

	listener := a.Additional[0]

	if listener.Port > 1 {
		port = listener.Port
	}

	if len(listener.AdvertisedPorts) > 1 {
		port = listener.AdvertisedPorts[replica]
	} else if len(listener.AdvertisedPorts) == 1 {
		port = listener.AdvertisedPorts[0]
	}

	return port
}

func (a *API) configEntries() map[string]any {
	var listeners []map[string]any
	var tlsEntries []map[string]any

	for _, listener := range a.Listeners() {
		entry := map[string]any{
			"name":    listener.Name,
			"address": listener.Address,
			"port":    listener.Port,
		}
		if listener.AuthenticationMethod != "" {
			entry["authentication_method"] = listener.AuthenticationMethod
		}
		listeners = append(listeners, entry)

		if listener.TLS != nil {
			tlsEntry := listener.TLS.configEntry()
			tlsEntry["name"] = listener.Name
			tlsEntries = append(tlsEntries, tlsEntry)
		}
	}

	entries := map[string]any{a.Kind.ConfigKey(): listeners}

	if len(tlsEntries) > 0 {
		entries[a.Kind.TLSConfigKey()] = tlsEntries
	}

	return entries
}

// Listener is one address that an [API] binds. It is an entry in the
// redpanda.yaml listener list, with the address that it advertises.
type Listener struct {
	// Name is the listener name in redpanda.yaml and the suffix of its port
	// names.
	Name string

	Port    int32
	Address string

	// AuthenticationMethod includes the default of the API. If it is empty,
	// the key is not rendered.
	AuthenticationMethod string

	// The caller sets PortName and ContainerPortName. Use
	// [APIKind.ReservedPortName] for the reserved listener, and
	// [APIKind.PortName] and [APIKind.ContainerPortName] for the other
	// listeners.
	PortName          string
	ContainerPortName string

	// AppProtocol is the appProtocol of each Service port that publishes this
	// listener.
	AppProtocol *string

	// TLS is nil when this listener serves none.
	TLS *ListenerTLS

	// PrefixTemplate and AdvertisedPorts configure advertisement in
	// redpanda.yaml.
	PrefixTemplate string

	// AdvertisedPorts contains the ports as the user sets them. The advertised
	// address uses the replica as an index. [ServiceKindNodePort] and
	// [ServiceKindLoadBalancer] use the first port.
	AdvertisedPorts []int32
}

// AdvertisedPort returns the port that this listener advertises for replica.
func (l *Listener) AdvertisedPort(replica int32) int32 {
	if len(l.AdvertisedPorts) == 1 {
		return l.AdvertisedPorts[0]
	}
	if len(l.AdvertisedPorts) > 1 {
		// NB: No bounds check. If there are more replicas than advertised
		// ports, the render fails. An incorrect advertised port is worse.
		return l.AdvertisedPorts[replica]
	}
	return l.Port
}

// ListenerTLS is the TLS a listener serves, resolved against a [PKI] by the
// caller. Nothing in here is a name waiting to be looked up.
type ListenerTLS struct {
	// Server is the keypair that this listener presents.
	Server Keypair

	// Client is the keypair that this listener presents if it requires mTLS.
	Client *Keypair

	// RequireClientAuth is the rendered require_client_auth. The chart sets
	// the flag of the listener. The operator sets the flag of the certificate.
	RequireClientAuth bool

	// TrustStore, if set, is the truststore that this listener uses to verify
	// peers.
	TrustStore *TrustStore

	// TrustStoreFallback is the truststore if this listener has no truststore
	// and its certificate has no CA. If it is empty, the serving certificate
	// is the truststore.
	TrustStoreFallback string
}

// NewListenerTLS resolves a listener's TLS against pki.
func NewListenerTLS(pki *PKI, cert string, requireClientAuth bool, trustStore *TrustStore) *ListenerTLS {
	// NB: A [PKI] issues a client keypair if one listener of the certificate
	// requires mTLS. A listener that does not require mTLS must not present
	// it.
	var client *Keypair
	if requireClientAuth {
		client = pki.ClientKeypair(cert)
	}

	return &ListenerTLS{
		Server:            pki.ServerKeypair(cert),
		Client:            client,
		RequireClientAuth: requireClientAuth,
		TrustStore:        trustStore,
	}
}

// TrustStoreFile returns the file that this listener uses to verify its peers.
func (t *ListenerTLS) TrustStoreFile() string {
	if t.TrustStore != nil {
		return t.TrustStore.AbsolutePath()
	}

	if t.Server.CA != nil {
		return t.Server.CAFile()
	}

	if t.TrustStoreFallback != "" {
		return t.TrustStoreFallback
	}

	return t.Server.CertFile()
}

// ServerCAFile returns the file that a peer uses to verify this server. It
// does not use [ListenerTLS.TrustStoreFallback], because a client that trusts
// all public CAs trusts all servers.
func (t *ListenerTLS) ServerCAFile() string {
	if file := t.TrustStoreFile(); file != t.TrustStoreFallback {
		return file
	}

	return t.Server.CertFile()
}

func (t *ListenerTLS) configEntry() map[string]any {
	return map[string]any{
		"enabled":             true,
		"cert_file":           t.Server.CertFile(),
		"key_file":            t.Server.KeyFile(),
		"require_client_auth": t.RequireClientAuth,
		"truststore_file":     t.TrustStoreFile(),
	}
}

// TrustStore is a Secret or ConfigMap key that a listener uses as its
// truststore_file.
type TrustStore struct {
	ConfigMapKeyRef *corev1.ConfigMapKeySelector `json:"configMapKeyRef"`
	SecretKeyRef    *corev1.SecretKeySelector    `json:"secretKeyRef"`
}

// AbsolutePath returns the path of the truststore in the container.
func (t *TrustStore) AbsolutePath() string {
	return fmt.Sprintf("%s/%s", trustStoreMountPath, t.RelativePath())
}

// RelativePath returns the path of the truststore in the mount. The path
// includes the source kind, thus a ConfigMap and a Secret with the same name
// do not collide.
func (t *TrustStore) RelativePath() string {
	if t.ConfigMapKeyRef != nil {
		return fmt.Sprintf("configmaps/%s-%s", t.ConfigMapKeyRef.Name, t.ConfigMapKeyRef.Key)
	}
	return fmt.Sprintf("secrets/%s-%s", t.SecretKeyRef.Name, t.SecretKeyRef.Key)
}

// VolumeProjection projects the truststore key to [TrustStore.RelativePath].
func (t *TrustStore) VolumeProjection() corev1.VolumeProjection {
	if t.ConfigMapKeyRef != nil {
		return corev1.VolumeProjection{
			ConfigMap: &corev1.ConfigMapProjection{
				LocalObjectReference: corev1.LocalObjectReference{Name: t.ConfigMapKeyRef.Name},
				Items: []corev1.KeyToPath{{
					Key:  t.ConfigMapKeyRef.Key,
					Path: t.RelativePath(),
				}},
			},
		}
	}

	return corev1.VolumeProjection{
		Secret: &corev1.SecretProjection{
			LocalObjectReference: corev1.LocalObjectReference{Name: t.SecretKeyRef.Name},
			Items: []corev1.KeyToPath{{
				Key:  t.SecretKeyRef.Key,
				Path: t.RelativePath(),
			}},
		},
	}
}

func dedupKeyToPaths(items []corev1.KeyToPath) []corev1.KeyToPath {
	// NB: gotohelm does not support slices.Compact.
	seen := map[string]bool{}
	var deduped []corev1.KeyToPath

	for _, item := range items {
		if _, ok := seen[item.Key]; ok {
			continue
		}

		deduped = append(deduped, item)
		seen[item.Key] = true
	}

	return deduped
}
