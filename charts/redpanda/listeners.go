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
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	// trustStoreMountPath is where every truststore projects. Format paths
	// with [TrustStore]'s methods, not by hand.
	trustStoreMountPath = "/etc/truststores"

	// OSTrustStorePath is the container's own CA bundle.
	OSTrustStorePath = "/etc/ssl/certs/ca-certificates.crt"

	// InternalListenerName is the listener both renderers put in-cluster
	// traffic on. A convention of theirs; Redpanda gives the name no special
	// status.
	InternalListenerName = "internal"

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

// InternalPortName is the in-cluster listener's port name, which both
// renderers key off the API alone.
func (k APIKind) InternalPortName() string {
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

// The four orders below disagree, and each is pinned to rendered output.
// Reordering one is observable; aligning two is a rewrite.

// APIs is external Service port order. RPC is absent -- rpc_server is not an
// *_api key.
func (l *Listeners) APIs() []*API {
	return l.inOrder([]APIKind{AdminAPI, KafkaAPI, HTTPAPI, SchemaRegistryAPI})
}

// All is truststore projection order. RPC included: it writes a
// truststore_file like any other listener.
func (l *Listeners) All() []*API {
	return l.inOrder([]APIKind{KafkaAPI, AdminAPI, HTTPAPI, SchemaRegistryAPI, RPCAPI})
}

// Ports is container and headless Service port order. Container ports are part
// of the pod template, so reordering rolls every broker.
func (l *Listeners) Ports() []*API {
	return l.inOrder([]APIKind{AdminAPI, HTTPAPI, KafkaAPI, RPCAPI, SchemaRegistryAPI})
}

// Gateways is TLSRoute and SAN order. Reaches a serving certificate's
// dnsNames, so reordering rotates certificates.
func (l *Listeners) Gateways() []*API {
	return l.inOrder([]APIKind{KafkaAPI, HTTPAPI, AdminAPI, SchemaRegistryAPI})
}

// inOrder skips a kind the cluster doesn't bind rather than yielding a nil
// [API] the caller would have to guard.
func (l *Listeners) inOrder(kinds []APIKind) []*API {
	var apis []*API
	for _, kind := range kinds {
		if api, ok := l.ByKind[kind]; ok {
			apis = append(apis, api)
		}
	}
	return apis
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

	for _, api := range l.APIs() {
		addEntries(sections[api.Kind.ConfigSection()], api.configEntries())
	}
	addEntries(sections[RPCAPI.ConfigSection()], l.rpcConfigEntries())

	return sections
}

func addEntries(section map[string]any, entries map[string]any) {
	// NB: gotohelm supports neither maps.Copy nor ranging a map here.
	for _, key := range slices.Sorted(maps.Keys(entries)) {
		section[key] = entries[key]
	}
}

// rpcConfigEntries renders rpc_server and rpc_server_tls, which take one
// listener as a bare map rather than the named list the *_api keys carry.
func (l *Listeners) rpcConfigEntries() map[string]any {
	listener := l.RPC().InCluster()

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

// ContainerPorts returns the redpanda container's ports in [Listeners.Ports]
// order.
func (l *Listeners) ContainerPorts() []corev1.ContainerPort {
	var ports []corev1.ContainerPort

	for _, api := range l.Ports() {
		for _, listener := range api.Listeners {
			ports = append(ports, corev1.ContainerPort{
				Name:          listener.ContainerPortName,
				ContainerPort: listener.Port,
			})
		}
	}

	return ports
}

// InternalServicePorts returns the headless Service's ports: each API's
// in-cluster listener, in [Listeners.Ports] order.
func (l *Listeners) InternalServicePorts() []corev1.ServicePort {
	var ports []corev1.ServicePort

	for _, api := range l.Ports() {
		listener := api.InCluster()
		if !listener.Exposed {
			continue
		}

		ports = append(ports, corev1.ServicePort{
			Name:        listener.PortName,
			Protocol:    corev1.ProtocolTCP,
			AppProtocol: api.AppProtocol,
			Port:        listener.Port,
			TargetPort:  intstr.FromInt32(listener.Port),
		})
	}

	return ports
}

// NodePortServicePorts publishes each listener's own port on the node at its
// first advertised port.
//
// One of three external formulas that genuinely disagree. See
// [Listeners.LoadBalancerServicePorts] and [Listeners.ExternalServicePorts].
func (l *Listeners) NodePortServicePorts() []corev1.ServicePort {
	var ports []corev1.ServicePort

	for _, api := range l.APIs() {
		for _, listener := range api.External() {
			if !listener.Exposed || listener.Gateway != nil {
				continue
			}

			nodePort := listener.Port
			if len(listener.AdvertisedPorts) > 0 {
				nodePort = listener.AdvertisedPorts[0]
			}

			ports = append(ports, corev1.ServicePort{
				Name:        listener.PortName,
				Protocol:    corev1.ProtocolTCP,
				AppProtocol: api.AppProtocol,
				Port:        listener.Port,
				TargetPort:  intstr.FromInt32(listener.Port),
				NodePort:    nodePort,
			})
		}
	}

	return ports
}

// LoadBalancerServicePorts publishes the advertised port and targets the
// listener's own. The chart's formula.
//
// NB: nodePort wins, then the first advertised port, then the API's
// *in-cluster* port -- not the exposed listener's.
func (l *Listeners) LoadBalancerServicePorts() []corev1.ServicePort {
	var ports []corev1.ServicePort

	for _, api := range l.APIs() {
		inCluster := api.InCluster()

		for _, listener := range api.External() {
			if !listener.Exposed || listener.Gateway != nil {
				continue
			}

			port := inCluster.Port
			if len(listener.AdvertisedPorts) > 0 {
				port = listener.AdvertisedPorts[0]
			}
			if listener.NodePort != nil {
				port = *listener.NodePort
			}

			ports = append(ports, corev1.ServicePort{
				Name:        listener.PortName,
				Protocol:    corev1.ProtocolTCP,
				AppProtocol: api.AppProtocol,
				Port:        port,
				TargetPort:  intstr.FromInt32(listener.Port),
			})
		}
	}

	return ports
}

// ExternalServicePorts publishes the listener's bound port where
// [Listeners.LoadBalancerServicePorts] publishes the advertised one. The
// operator's formula, unused until it renders Services through this package.
func (l *Listeners) ExternalServicePorts() []corev1.ServicePort {
	var ports []corev1.ServicePort

	for _, api := range l.APIs() {
		for _, listener := range api.External() {
			if !listener.Exposed || listener.Gateway != nil {
				continue
			}

			ports = append(ports, corev1.ServicePort{
				Name:        listener.PortName,
				Protocol:    corev1.ProtocolTCP,
				AppProtocol: api.AppProtocol,
				Port:        listener.Port,
				TargetPort:  intstr.FromInt32(listener.Port),
			})
		}
	}

	return ports
}

// GatewayServicePorts returns the ClusterIP Service ports backing the
// TLSRoutes of every listener that opted into Gateway API.
//
// NB: [Listeners.APIs] order. Only the TLSRoutes and SANs follow
// [Listeners.Gateways].
func (l *Listeners) GatewayServicePorts() []corev1.ServicePort {
	var ports []corev1.ServicePort

	for _, api := range l.APIs() {
		for _, listener := range api.External() {
			if !listener.Exposed || listener.Gateway == nil {
				continue
			}

			ports = append(ports, corev1.ServicePort{
				Name:        listener.PortName,
				Protocol:    corev1.ProtocolTCP,
				AppProtocol: api.AppProtocol,
				Port:        listener.Port,
				TargetPort:  intstr.FromInt32(listener.Port),
			})
		}
	}

	return ports
}

// TrustStores returns every active truststore in [Listeners.All] order.
func (l *Listeners) TrustStores() []*TrustStore {
	var stores []*TrustStore

	for _, api := range l.All() {
		for _, listener := range api.Listeners {
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

	// AppProtocol annotates every Service port this API renders.
	AppProtocol *string

	// Listeners is every address this API binds, in redpanda.yaml order.
	//
	// One list, because Redpanda draws no in-cluster/external distinction.
	// Where Kubernetes does, it is a derived view: [API.InCluster] and
	// [API.External].
	Listeners []Listener
}

// InCluster is the listener named [InternalListenerName], which a broker's own
// clients, the probes, the sidecar and the headless Service all reach it on.
//
// Nil when the API has none, which every render path dereferences unguarded.
// Both resolvers always emit one; a panic here beats a zero-valued listener
// rendering port 0.
func (a *API) InCluster() *Listener {
	for _, listener := range a.Listeners {
		if listener.Name == InternalListenerName {
			return &listener
		}
	}
	return nil
}

// External is every listener other than [API.InCluster].
func (a *API) External() []Listener {
	var external []Listener

	for _, listener := range a.Listeners {
		if listener.Name == InternalListenerName {
			continue
		}
		external = append(external, listener)
	}

	return external
}

// RPKClientTLS is rpk's TLS type for this API, nil when its in-cluster
// listener serves none. Nil, not empty: callers disagree on what absent looks
// like in YAML, so each normalises its own.
func (a *API) RPKClientTLS() map[string]any {
	listener := a.InCluster()

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
// this API, nil when its in-cluster listener serves none. Distinct from
// [API.RPKClientTLS]: rpk and Redpanda read different keys for it.
func (a *API) BrokerClientTLS() map[string]any {
	listener := a.InCluster()

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

// CurlFlags reaches this API's in-cluster listener, empty when it serves no
// TLS.
func (a *API) CurlFlags() string {
	listener := a.InCluster()

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
// NB: starts from the *in-cluster* port and guards on the external one being
// > 1, not > 0, so a port-less listener falls through.
func (a *API) ProfileAdvertisedPort(replica int32) int32 {
	inCluster := a.InCluster()
	port := inCluster.Port

	external := a.External()
	if len(external) < 1 {
		return port
	}

	listener := external[0]

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

	for _, listener := range a.Listeners {
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
	// names. [InternalListenerName] for the in-cluster listener.
	Name string

	Port    int32
	Address string

	// AuthenticationMethod is fully resolved, the API's default folded in.
	// Empty omits the key.
	AuthenticationMethod string

	// PortName and ContainerPortName are resolved by the caller: the in-cluster
	// listener is named for its API alone ("schemaregistry") while the rest
	// carry both ("schema-public"). Build them with [PortName] and
	// [ContainerPortName].
	PortName          string
	ContainerPortName string

	// TLS is nil when this listener serves none.
	TLS *ListenerTLS

	// PrefixTemplate, AdvertisedPorts and Gateway are advertisement, which is
	// redpanda.yaml -- they survive [Listener.Exposed] being false.
	PrefixTemplate string

	// Raw: the advertised address indexes it by replica while two Service
	// formulas read its first element.
	AdvertisedPorts []int32

	// NodePort pins what [Listeners.LoadBalancerServicePorts] publishes.
	NodePort *int32

	// Gateway is nil unless this listener opted into Gateway API. Its presence
	// moves it off the NodePort/LoadBalancer Services and onto the gateway
	// ones.
	Gateway *GatewayRoute

	// Exposed is whether Kubernetes publishes this listener: its Service port,
	// TLSRoute and gateway SAN. False still binds, advertises and declares a
	// container port -- what values.yaml promises for external.enabled false.
	Exposed bool
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

// GatewayRoute is a listener's Gateway API routing. Hostnames arrive fully
// rendered; nothing downstream sees a template.
type GatewayRoute struct {
	// Host is the bootstrap TLSRoute's SNI name.
	Host string

	// BrokerHosts is one SNI name per broker, in global ordinal order.
	BrokerHosts []string
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
