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
	// trustStoreMountPath is where every truststore projects. Format paths
	// with [TrustStore]'s methods, not by hand.
	trustStoreMountPath = "/etc/truststores"

	// OSTrustStorePath is the container's own CA bundle.
	OSTrustStorePath = "/etc/ssl/certs/ca-certificates.crt"

	// ReservedListenerName is the name of [API.Reserved]. Redpanda gives the
	// name no special status.
	ReservedListenerName = "internal"

	trustStoreVolumeName = "truststores"
)

// APIKind identifies one of redpanda.yaml's listener sets. Every key an [API]
// renders under derives from it.
type APIKind string

const (
	AdminAPI          APIKind = "admin"
	KafkaAPI          APIKind = "kafka"
	HTTPAPI           APIKind = "http"
	SchemaRegistryAPI APIKind = "schema"
	RPCAPI            APIKind = "rpc"
)

// ConfigKey is the redpanda.yaml key this kind's listener list renders under.
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

// TLSConfigKey is [APIKind.ConfigKey]'s *_tls twin.
func (k APIKind) TLSConfigKey() string {
	// NB: admin is the irregular pair -- a bare "admin", but "admin_api_tls".
	if k == AdminAPI {
		return "admin_api_tls"
	}
	return fmt.Sprintf("%s_tls", k.ConfigKey())
}

// ConfigSection is the top level redpanda.yaml key [APIKind.ConfigKey] nests
// under: "redpanda", "pandaproxy", or "schema_registry".
func (k APIKind) ConfigSection() string {
	if k == HTTPAPI {
		return "pandaproxy"
	}
	if k == SchemaRegistryAPI {
		return "schema_registry"
	}
	return "redpanda"
}

// AdvertisedConfigKey is where the configurator writes this kind's advertised
// addresses. Empty for admin, schema and rpc, which are not advertised.
func (k APIKind) AdvertisedConfigKey() string {
	if k != KafkaAPI && k != HTTPAPI {
		return ""
	}
	return fmt.Sprintf("advertised_%s", k.ConfigKey())
}

// AdvertisedConfigSection is where [APIKind.AdvertisedConfigKey] nests, empty
// on the same condition.
func (k APIKind) AdvertisedConfigSection() string {
	if k.AdvertisedConfigKey() == "" {
		return ""
	}
	return k.ConfigSection()
}

// ReservedPortName is the port name of [API.Reserved], which both renderers
// key off the API alone.
func (k APIKind) ReservedPortName() string {
	if k == SchemaRegistryAPI {
		return "schemaregistry"
	}
	return string(k)
}

// PortName is a Service port name: this kind and the listener's name.
func (k APIKind) PortName(listener string) string {
	return fmt.Sprintf("%s-%s", k, listener)
}

// ContainerPortName is [APIKind.PortName] narrowed to what a container port
// accepts.
//
// NB: lowercased before truncating, since port names are RFC 1123 labels. Caps
// at 15 characters, so two listeners agreeing on their first eight lowercased
// characters collide and Kubernetes rejects the duplicate.
func (k APIKind) ContainerPortName(listener string) string {
	return fmt.Sprintf("%s-%.8s", k, strings.ToLower(listener))
}

// Listeners is a cluster's resolved listener set: what Redpanda binds, what
// Kubernetes exposes, and which certificate each listener serves.
type Listeners struct {
	// ByKind is every API the cluster binds. Prefer the named accessors and
	// the orderings below.
	ByKind map[APIKind]*API
}

// NewListeners keys apis by their [API.Kind].
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
	// NB: APIs share a section but never an entry key, so each writes straight
	// into a pre-created one.
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

// TrustStoreVolume projects every truststore into one Volume: ConfigMap
// sources before Secret ones, each group name-sorted. Nil when there are
// none.
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

// TrustStoreMount pairs with [Listeners.TrustStoreVolume], nil on the same
// condition.
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

// API is one of redpanda.yaml's listener sets: an *_api key, its *_tls twin,
// and the listeners it binds. rpc_server is modelled as one for uniformity.
type API struct {
	// Kind prefixes port and TLSRoute names, and every redpanda.yaml key this
	// API renders under derives from it.
	Kind APIKind

	// Reserved is the listener that the chart and the operator use: the probes,
	// the sidecar, rpk, the clients of Redpanda itself, and the headless
	// Service. Its name is [ReservedListenerName]. It is nil if the API binds no
	// reserved listener.
	Reserved *Listener

	// Additional contains the other listeners, in redpanda.yaml order.
	Additional []Listener
}

// Listeners returns every listener of the API, in redpanda.yaml order:
// [API.Reserved], then [API.Additional].
func (a *API) Listeners() []Listener {
	var listeners []Listener
	if a.Reserved != nil {
		listeners = append(listeners, *a.Reserved)
	}
	return append(listeners, a.Additional...)
}

// RPKClientTLS is rpk's TLS type for this API, nil when its reserved
// listener serves none. Nil, not empty: callers disagree on what absent looks
// like in YAML, so each normalises its own.
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

// BrokerClientTLS is the broker_tls block Redpanda's own clients use to reach
// this API, nil when its reserved listener serves none. Distinct from
// [API.RPKClientTLS]: rpk and Redpanda read different keys for it.
func (a *API) BrokerClientTLS() map[string]any {
	listener := a.Reserved

	tls := listener.TLS
	if tls == nil {
		return nil
	}

	cfg := map[string]any{
		"enabled":             true,
		"require_client_auth": tls.RequireClientAuth,
		// NB: truststore_file here is synonymous with ca_file in the rpk
		// configuration. The difference being that redpanda does NOT read the
		// ca_file key.
		"truststore_file": tls.ServerCAFile(),
	}

	if kp := tls.Client; kp != nil {
		cfg["cert_file"] = kp.CertFile()
		cfg["key_file"] = kp.KeyFile()
	}

	return cfg
}

// CurlFlags reaches the reserved listener of this API, empty when it serves no
// TLS.
func (a *API) CurlFlags() string {
	listener := a.Reserved

	tls := listener.TLS
	if tls == nil {
		return ""
	}

	if kp := tls.Client; kp != nil {
		path := kp.MountPath()
		return fmt.Sprintf("--cacert %s/ca.crt --cert %s/tls.crt --key %s/tls.key", path, path, path)
	}

	return fmt.Sprintf("--cacert %s", tls.ServerCAFile())
}

// ProfileAdvertisedPort is the port an rpk profile tells external clients to
// dial for replica. Differs from [Listener.AdvertisedPort], which the
// configurator uses.
//
// NB: starts from the *reserved* port and guards on the additional one being
// > 1, not > 0, so a port-less listener falls through.
func (a *API) ProfileAdvertisedPort(replica int32) int32 {
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

// Listener is one address an [API] binds: an entry in its redpanda.yaml
// listener list, plus the address it advertises for that entry.
type Listener struct {
	// Name is the listener's name in redpanda.yaml and the suffix of its port
	// names. [ReservedListenerName] for the reserved listener.
	Name string

	Port    int32
	Address string

	// AuthenticationMethod is fully resolved, the API's default folded in.
	// Empty omits the key.
	AuthenticationMethod string

	// PortName and ContainerPortName are resolved by the caller: the reserved
	// listener is named for its API alone ("schemaregistry") while the rest
	// carry both ("schema-public"). Build them with [PortName] and
	// [ContainerPortName].
	PortName          string
	ContainerPortName string

	// AppProtocol is the appProtocol of each Service port that publishes this
	// listener.
	AppProtocol *string

	// TLS is nil when this listener serves none.
	TLS *ListenerTLS

	// PrefixTemplate and AdvertisedPorts configure advertisement in
	// redpanda.yaml. They apply also when no Service publishes the listener.
	PrefixTemplate string

	// AdvertisedPorts contains the ports as the user sets them. The advertised
	// address uses the replica as an index. [ServiceKindNodePort] and
	// [ServiceKindLoadBalancer] use the first port.
	AdvertisedPorts []int32
}

// AdvertisedPort is what this listener advertises to replica: its single
// advertised port, the replica.th of several, or its own.
//
// NB: the multi-port branch indexes unguarded, so more replicas than
// advertised ports fails the render. Deliberate -- advertising the wrong port
// silently is worse.
func (l *Listener) AdvertisedPort(replica int32) int32 {
	if len(l.AdvertisedPorts) == 1 {
		return l.AdvertisedPorts[0]
	}
	if len(l.AdvertisedPorts) > 1 {
		return l.AdvertisedPorts[replica]
	}
	return l.Port
}

// ListenerTLS is the TLS a listener serves, resolved against a [PKI] by the
// caller. Nothing in here is a name waiting to be looked up.
type ListenerTLS struct {
	// Server is the keypair this listener presents.
	Server Keypair

	// Client is the keypair it presents when it requires mTLS. See
	// [NewListenerTLS] for why it is not simply the certificate's.
	Client *Keypair

	// RequireClientAuth is the rendered require_client_auth. Resolved, not
	// derived: the chart takes the listener's flag, the operator the
	// certificate's.
	RequireClientAuth bool

	// TrustStore overrides what this listener verifies peers against.
	TrustStore *TrustStore

	// TrustStoreFallback is the last resort when this listener configures no
	// truststore and its certificate ships no CA. Empty falls through to the
	// serving certificate, which is the fail-closed answer; the chart sets
	// [OSTrustStorePath], which values.yaml documents under caEnabled.
	TrustStoreFallback string
}

// NewListenerTLS resolves a listener's TLS against pki.
func NewListenerTLS(pki *PKI, cert string, requireClientAuth bool, trustStore *TrustStore) *ListenerTLS {
	// NB: gated on the listener's flag, not the certificate's. A [PKI] issues a
	// client keypair whenever *any* listener sharing the certificate requires
	// mTLS, and one that doesn't must not present it.
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

// TrustStoreFile is what a listener verifies its peers against: its explicit
// truststore, else its certificate's issuing CA, else
// [ListenerTLS.TrustStoreFallback].
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

// ServerCAFile is what a peer verifies this server against:
// [ListenerTLS.TrustStoreFile] with the serving certificate as the last resort
// instead. A client falling back to every public CA trusts anyone.
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

// TrustStore maps a value on a Secret or ConfigMap to a listener's
// truststore_file.
type TrustStore struct {
	ConfigMapKeyRef *corev1.ConfigMapKeySelector `json:"configMapKeyRef"`
	SecretKeyRef    *corev1.SecretKeySelector    `json:"secretKeyRef"`
}

// AbsolutePath is the truststore's path inside the container.
func (t *TrustStore) AbsolutePath() string {
	return fmt.Sprintf("%s/%s", trustStoreMountPath, t.RelativePath())
}

// RelativePath is the truststore's path within the mount, namespaced by source
// kind so a ConfigMap and a Secret sharing a name cannot collide.
func (t *TrustStore) RelativePath() string {
	if t.ConfigMapKeyRef != nil {
		return fmt.Sprintf("configmaps/%s-%s", t.ConfigMapKeyRef.Name, t.ConfigMapKeyRef.Key)
	}
	return fmt.Sprintf("secrets/%s-%s", t.SecretKeyRef.Name, t.SecretKeyRef.Key)
}

// VolumeProjection projects the truststore's key at [TrustStore.RelativePath].
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
	// NB: a seen-set rather than slices.Compact, which gotohelm dislikes.
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
