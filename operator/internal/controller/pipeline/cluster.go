// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package pipeline

import (
	"context"
	"strings"
	"sync"

	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/common-go/kube"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	ossconv "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2/conversion"
	"github.com/redpanda-data/redpanda-operator/pkg/ir"
)

// clusterConnCache memoizes the resolved clusterConnection per Redpanda CR.
//
// Resolving a clusterRef runs the full charts/redpanda render closure
// (ConvertV2ToRenderState + AsStaticConfigSource), which is by far the most
// expensive part of a pipeline reconcile. The result depends only on the
// Redpanda CR's spec, so N pipelines pointing at the same cluster would
// otherwise render it N times — and again on every sync-interval/pod-churn
// reconcile. Keying by the CR's metadata.generation (which bumps only on a spec
// change, unlike resourceVersion which also bumps on status writes) means one
// render per cluster spec, shared by all referencing pipelines; the
// parent-Redpanda watch already re-enqueues those pipelines when the spec
// changes, so a stale entry is naturally superseded on the next generation.
//
// A 100-pipeline EKS run measured a cold re-render of all pipelines peaking at
// ~124m CPU; with this cache that collapses to a single render plus 99 map hits.
type clusterConnCache struct {
	mu      sync.Mutex
	entries map[string]clusterConnCacheEntry // "namespace/name" -> entry
}

type clusterConnCacheEntry struct {
	// uid pins the entry to a specific incarnation of the CR. A deleted and
	// recreated cluster restarts at generation 1, so generation alone would
	// keep serving the old cluster's brokers/TLS/SASL after a recreate.
	uid        types.UID
	generation int64
	conn       *clusterConnection
}

func newClusterConnCache() *clusterConnCache {
	return &clusterConnCache{entries: map[string]clusterConnCacheEntry{}}
}

// get returns the cached connection for a cluster when the cached entry matches
// the CR's current UID and generation.
func (c *clusterConnCache) get(namespace, name string, uid types.UID, generation int64) (*clusterConnection, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[namespace+"/"+name]
	if ok && e.uid == uid && e.generation == generation {
		return e.conn, true
	}
	return nil, false
}

// put stores the connection for a cluster at the given UID + generation,
// replacing any older entry (so the cache holds at most one entry per cluster).
func (c *clusterConnCache) put(namespace, name string, uid types.UID, generation int64, conn *clusterConnection) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[namespace+"/"+name] = clusterConnCacheEntry{uid: uid, generation: generation, conn: conn}
}

// evict drops the cached entry for a cluster. Called from the Redpanda watch's
// delete handler so entries for deleted clusters don't accumulate (and a
// recreate under the same name starts from a clean slate).
func (c *clusterConnCache) evict(namespace, name string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, namespace+"/"+name)
}

// clusterConnection holds the resolved connection details for a Redpanda cluster.
type clusterConnection struct {
	// Brokers is the list of internal Kafka broker addresses (host:port).
	Brokers []string
	// TLS holds TLS configuration if the cluster has TLS enabled.
	TLS *clusterTLS
	// SASL holds SASL credentials if the cluster has authentication enabled.
	SASL *clusterSASL
}

// clusterTLS holds TLS configuration resolved from a Redpanda cluster. A
// non-nil clusterTLS means "connect with TLS"; the CA references are optional
// so publicly-issued certificates (no custom CA) are representable.
type clusterTLS struct {
	// CACertSecretRef points to the Secret and key containing the CA certificate.
	CACertSecretRef *corev1.SecretKeySelector
	// CACertConfigMapRef points to the ConfigMap and key containing the CA
	// certificate. Only one of CACertSecretRef / CACertConfigMapRef is set.
	CACertConfigMapRef *corev1.ConfigMapKeySelector
	// ClientCertSecretRef / ClientCertConfigMapRef point to the client
	// certificate presented to mTLS listeners (requireClientAuth on a
	// referenced cluster, or cert/key on staticConfiguration). At most one is
	// set; both nil when the listener doesn't require client auth.
	ClientCertSecretRef    *corev1.SecretKeySelector
	ClientCertConfigMapRef *corev1.ConfigMapKeySelector
	// ClientKeySecretRef points to the client private key paired with the
	// client certificate. Private keys must come from Secrets.
	ClientKeySecretRef *corev1.SecretKeySelector
	// InsecureSkipVerify disables server certificate verification.
	InsecureSkipVerify bool
}

// caCertKey returns the key under which the CA certificate is projected into
// the TLS volume, or "" when no custom CA is configured.
func (t *clusterTLS) caCertKey() string {
	switch {
	case t == nil:
		return ""
	case t.CACertSecretRef != nil:
		return t.CACertSecretRef.Key
	case t.CACertConfigMapRef != nil:
		return t.CACertConfigMapRef.Key
	}
	return ""
}

// hasClientCert reports whether a client certificate + key pair is configured
// (i.e. the connection is mTLS).
func (t *clusterTLS) hasClientCert() bool {
	if t == nil {
		return false
	}
	return (t.ClientCertSecretRef != nil || t.ClientCertConfigMapRef != nil) && t.ClientKeySecretRef != nil
}

// clusterSASL holds the SASL credentials resolved from the cluster source:
// the bootstrap user for clusterRef, or the inline credentials for
// staticConfiguration. Exactly one of PasswordRef / PasswordConfigMapRef /
// PasswordValue carries the password.
type clusterSASL struct {
	Mechanism            string
	Username             string
	PasswordRef          *corev1.SecretKeySelector
	PasswordConfigMapRef *corev1.ConfigMapKeySelector
	// PasswordValue is an inline password from
	// cluster.staticConfiguration.kafka.sasl.password.inline. The renderer
	// mirrors it into a Pipeline-owned Secret (<pipeline>-sasl) rather than
	// embedding it as a literal EnvVar value — pod specs are readable by
	// anyone with pods/deployments get and land in cluster backups.
	PasswordValue string
}

// envVars projects these credentials as REDPANDA_SASL_* env vars, the same
// names userCredentials uses, so the operator-generated `sasl:` block in
// connect.yaml resolves regardless of which source supplied the identity.
// saslSecretName is the Pipeline-owned Secret that mirrors an inline password
// (see render.saslSecret).
func (s *clusterSASL) envVars(saslSecretName string) []corev1.EnvVar {
	if s == nil {
		return nil
	}
	out := []corev1.EnvVar{
		{Name: "REDPANDA_SASL_USERNAME", Value: s.Username},
		{Name: "REDPANDA_SASL_MECHANISM", Value: s.Mechanism},
	}
	switch {
	case s.PasswordRef != nil:
		out = append(out, corev1.EnvVar{
			Name:      "REDPANDA_SASL_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{SecretKeyRef: s.PasswordRef},
		})
	case s.PasswordConfigMapRef != nil:
		out = append(out, corev1.EnvVar{
			Name:      "REDPANDA_SASL_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{ConfigMapKeyRef: s.PasswordConfigMapRef},
		})
	case s.PasswordValue != "":
		out = append(out, corev1.EnvVar{
			Name: "REDPANDA_SASL_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: saslSecretName},
				Key:                  saslSecretKey,
			}},
		})
	}
	return out
}

// userCredentials holds the SASL identity a Pipeline authenticates as when
// it is bound to a Redpanda cluster via .userRef. Distinct from
// clusterSASL.bootstrap; this is a per-pipeline named SCRAM user managed by
// the User CRD with ACLs scoped to what the pipeline actually reads/writes.
type userCredentials struct {
	Mechanism   string
	Username    string
	PasswordRef *corev1.SecretKeySelector
}

// envVars returns the corev1 env-var projections for these credentials.
// Pipelines reference these as ${REDPANDA_SASL_USERNAME} etc. in their
// configYaml, and the operator-generated `redpanda` block in connect.yaml
// uses the same names so both paths converge on the same Secret backing.
func (uc *userCredentials) envVars() []corev1.EnvVar {
	if uc == nil {
		return nil
	}
	out := []corev1.EnvVar{
		{Name: "REDPANDA_SASL_USERNAME", Value: uc.Username},
		{Name: "REDPANDA_SASL_MECHANISM", Value: uc.Mechanism},
	}
	if uc.PasswordRef != nil {
		out = append(out, corev1.EnvVar{
			Name: "REDPANDA_SASL_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: uc.PasswordRef,
			},
		})
	}
	return out
}

// BrokersString returns the broker list as a comma-separated string.
func (c *clusterConnection) BrokersString() string {
	return strings.Join(c.Brokers, ",")
}

// resolveUserRef resolves the Pipeline's userRef to a SCRAM identity backed
// by the User CR's password Secret. Returns nil if no userRef is set.
//
// The referenced User CR must:
//   - exist in the same namespace as the Pipeline
//   - have spec.authentication populated
//   - have spec.authentication.password.valueFrom.secretKeyRef set (inline
//     plaintext passwords are rejected; production deployments must use a
//     Secret-backed value so password rotation is auditable)
//   - have a password Secret that actually exists and contains the referenced
//     key. Without this check the pipeline pod would be created anyway and
//     wedge in CreateContainerConfigError; validating here surfaces the
//     problem as UserRef=False instead.
func resolveUserRef(ctx context.Context, ctl *kube.Ctl, pipeline *redpandav1alpha2.Pipeline) (*userCredentials, error) {
	if pipeline.Spec.UserRef == nil {
		return nil, nil
	}

	ref := pipeline.Spec.UserRef
	var user redpandav1alpha2.User
	if err := ctl.Get(ctx, kube.ObjectKey{Name: ref.Name, Namespace: pipeline.Namespace}, &user); err != nil {
		return nil, errors.Wrapf(err, "failed to resolve userRef %q", ref.Name)
	}

	if user.Spec.Authentication == nil {
		return nil, errors.Newf("userRef %q has no spec.authentication; the Pipeline cannot authenticate to Redpanda", ref.Name)
	}
	if user.Spec.Authentication.Password.ValueFrom == nil || user.Spec.Authentication.Password.ValueFrom.SecretKeyRef == nil {
		return nil, errors.Newf("userRef %q has no spec.authentication.password.valueFrom.secretKeyRef; pipelines require a Secret-backed password for auditable rotation", ref.Name)
	}

	passwordRef := user.Spec.Authentication.Password.ValueFrom.SecretKeyRef
	var secret corev1.Secret
	if err := ctl.Get(ctx, kube.ObjectKey{Name: passwordRef.Name, Namespace: pipeline.Namespace}, &secret); err != nil {
		return nil, errors.Wrapf(err, "userRef %q: failed to read password Secret %q", ref.Name, passwordRef.Name)
	}
	if _, ok := secret.Data[passwordRef.Key]; !ok {
		return nil, errors.Newf("userRef %q: password Secret %q has no key %q", ref.Name, passwordRef.Name, passwordRef.Key)
	}

	mechanism := "SCRAM-SHA-512"
	if t := user.Spec.Authentication.Type; t != nil && *t != "" {
		mechanism = strings.ToUpper(string(*t))
	}

	return &userCredentials{
		Mechanism:   mechanism,
		Username:    user.Name,
		PasswordRef: passwordRef,
	}, nil
}

// resolveClusterSource resolves the Pipeline's clusterRef to connection details.
// Returns nil if no clusterRef is set. The expensive chart render is memoized
// per Redpanda cluster (by spec generation) via the controller's
// clusterConnCache, so repeated reconciles and many pipelines sharing one
// cluster don't each re-render it.
func (c *Controller) resolveClusterSource(ctx context.Context, pipeline *redpandav1alpha2.Pipeline) (*clusterConnection, error) {
	if pipeline.Spec.ClusterSource == nil {
		return nil, nil
	}

	// clusterRef takes precedence over staticConfiguration (matching the
	// ClusterSource contract). The static path is pure translation of
	// user-supplied values — no API reads, so no caching either.
	if pipeline.Spec.ClusterSource.ClusterRef == nil {
		if static := pipeline.Spec.ClusterSource.StaticConfiguration; static != nil {
			return staticClusterConnection(static)
		}
		return nil, nil
	}

	ref := pipeline.Spec.ClusterSource.ClusterRef

	// Pipelines only support referencing a Redpanda CR in their own
	// namespace: the resolved connection is projected into the pipeline pod
	// as Secret/ConfigMap references, and pods cannot mount objects from
	// other namespaces. CEL on PipelineSpec rejects these fields at admission;
	// this guard covers CRs admitted before that rule existed. Explicitly
	// spelling out the default group/kind is tolerated.
	if ns := ref.GetNamespace(pipeline.Namespace); ns != pipeline.Namespace {
		return nil, errors.Newf("clusterRef.namespace %q is not supported for pipelines; the referenced Redpanda must live in the Pipeline's namespace %q (its TLS/SASL Secrets cannot be mounted across namespaces)", ns, pipeline.Namespace)
	}
	if !ref.IsV2() {
		return nil, errors.Newf("clusterRef %q: group %q / kind %q is not supported for pipelines; only cluster.redpanda.com/Redpanda references are supported", ref.Name, ref.GetGroup(), ref.GetKind())
	}

	// Fetch the OSS Redpanda CR (cheap — served from the informer cache).
	var rp redpandav1alpha2.Redpanda
	if err := c.Ctl.Get(ctx, kube.ObjectKey{Name: ref.Name, Namespace: pipeline.Namespace}, &rp); err != nil {
		return nil, errors.Wrapf(err, "failed to resolve clusterRef %q", ref.Name)
	}

	// Cache hit: the chart render for this cluster spec is already resolved.
	// The UID pins the entry to this incarnation of the CR so a delete +
	// recreate under the same name (which restarts at generation 1) can't
	// serve the old cluster's connection details.
	if conn, ok := c.clusterConns.get(rp.Namespace, rp.Name, rp.UID, rp.Generation); ok {
		return conn, nil
	}

	// Resolve its Kafka connection details by converting it to a render state
	// (faithful to redpanda-operator #1337). This uses the upstream
	// charts/redpanda render machinery via the OSS operator module dependency;
	// outputs are plain broker strings + corev1 secret refs, so no upstream
	// types leak into the enterprise surface. This is the expensive step the
	// cache exists to avoid repeating.
	state, err := ossconv.ConvertV2ToRenderState(nil, &ossconv.V2Defaulters{
		RedpandaImage: func(ri *redpandav1alpha2.RedpandaImage) *redpandav1alpha2.RedpandaImage { return ri },
		SidecarImage:  func(ri *redpandav1alpha2.RedpandaImage) *redpandav1alpha2.RedpandaImage { return ri },
	}, &rp, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to convert Redpanda CR to render state")
	}

	cfg := state.AsStaticConfigSource()

	conn := &clusterConnection{}
	if cfg.Kafka != nil {
		conn.Brokers = cfg.Kafka.Brokers

		if cfg.Kafka.TLS != nil {
			tlsConf, err := clusterTLSFromIR(rp.Name, cfg.Kafka.TLS)
			if err != nil {
				return nil, err
			}
			conn.TLS = tlsConf
		}

		if cfg.Kafka.SASL != nil {
			conn.SASL = &clusterSASL{
				Mechanism: string(cfg.Kafka.SASL.Mechanism),
				Username:  cfg.Kafka.SASL.Username,
			}
			if cfg.Kafka.SASL.Password != nil && cfg.Kafka.SASL.Password.SecretKeyRef != nil {
				conn.SASL.PasswordRef = cfg.Kafka.SASL.Password.SecretKeyRef
			}
		}
	}

	c.clusterConns.put(rp.Namespace, rp.Name, rp.UID, rp.Generation, conn)
	return conn, nil
}

// clusterTLSFromIR maps the Kafka TLS material the chart render produced for a
// referenced cluster onto the pipeline's mountable clusterTLS. The chart's
// InternalTLS.ToCommonTLS legitimately emits three shapes that all must be
// honored:
//
//   - a Secret-backed CA (cert-manager issued, key ca.crt — or the server
//     cert itself under cert.crt when the bundle carries no CA)
//   - a ConfigMap-backed truststore (listeners.kafka.tls.trustStore)
//   - client cert + key Secrets when the listener sets requireClientAuth
//     (mTLS) — dropping these would dial an mTLS listener without a client
//     certificate.
func clusterTLSFromIR(clusterName string, tls *ir.CommonTLS) (*clusterTLS, error) {
	out := &clusterTLS{InsecureSkipVerify: tls.InsecureSkipTLSVerify}

	if ca := tls.CaCert; ca != nil {
		switch {
		case ca.SecretKeyRef != nil:
			out.CACertSecretRef = ca.SecretKeyRef
		case ca.ConfigMapKeyRef != nil:
			out.CACertConfigMapRef = ca.ConfigMapKeyRef
		default:
			return nil, errors.Newf("clusterRef %q: the cluster's Kafka CA is not backed by a Secret or ConfigMap and cannot be mounted into the pipeline pod", clusterName)
		}
	}

	// requireClientAuth listeners hand out a client keypair; both halves are
	// Secret-backed in every shape the chart renders.
	if (tls.Cert == nil) != (tls.Key == nil) {
		return nil, errors.Newf("clusterRef %q: the cluster's Kafka listener TLS specifies only one of client cert/key", clusterName)
	}
	if tls.Cert != nil {
		switch {
		case tls.Cert.SecretKeyRef != nil:
			out.ClientCertSecretRef = tls.Cert.SecretKeyRef
		case tls.Cert.ConfigMapKeyRef != nil:
			out.ClientCertConfigMapRef = tls.Cert.ConfigMapKeyRef
		default:
			return nil, errors.Newf("clusterRef %q: the cluster's Kafka client certificate is not backed by a Secret or ConfigMap and cannot be mounted into the pipeline pod", clusterName)
		}
		if tls.Key.SecretKeyRef == nil {
			return nil, errors.Newf("clusterRef %q: the cluster's Kafka client key is not backed by a Secret and cannot be mounted into the pipeline pod", clusterName)
		}
		out.ClientKeySecretRef = tls.Key.SecretKeyRef
	}

	return out, nil
}

// staticClusterConnection translates a user-supplied
// cluster.staticConfiguration into a clusterConnection: brokers, TLS, and
// SASL credentials, all hard-coded on the Pipeline spec rather than resolved
// from a Redpanda CR.
func staticClusterConnection(static *redpandav1alpha2.StaticConfigurationSource) (*clusterConnection, error) {
	kafka := static.Kafka
	if kafka == nil {
		return nil, errors.New("cluster.staticConfiguration.kafka is required: pipelines connect to the Kafka API")
	}
	if len(kafka.Brokers) == 0 {
		return nil, errors.New("cluster.staticConfiguration.kafka.brokers must not be empty")
	}

	conn := &clusterConnection{Brokers: kafka.Brokers}

	if tls := kafka.TLS; tls != nil && staticTLSEnabled(tls) {
		conn.TLS = &clusterTLS{InsecureSkipVerify: tls.InsecureSkipTLSVerify}
		switch {
		case tls.CaCert != nil && tls.CaCert.SecretKeyRef != nil:
			conn.TLS.CACertSecretRef = tls.CaCert.SecretKeyRef
		case tls.CaCert != nil && tls.CaCert.ConfigMapKeyRef != nil:
			conn.TLS.CACertConfigMapRef = tls.CaCert.ConfigMapKeyRef
		case tls.CaCert != nil:
			// Inline / externalSecretRef CA material has no mountable backing
			// object; reject it rather than silently connecting without the CA.
			return nil, errors.New("cluster.staticConfiguration.kafka.tls.caCert must use secretKeyRef or configMapKeyRef; store the CA certificate in a Secret or ConfigMap")
		case tls.DeprecatedCaCert != nil: //nolint:staticcheck // deprecated field still accepted for spec compatibility
			key := tls.DeprecatedCaCert.Key //nolint:staticcheck // ditto
			if key == "" {
				key = "ca.crt"
			}
			conn.TLS.CACertSecretRef = &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: tls.DeprecatedCaCert.Name}, //nolint:staticcheck // ditto
				Key:                  key,
			}
		}

		if err := staticClientCert(tls, conn.TLS); err != nil {
			return nil, err
		}
	}

	if sasl := kafka.SASL; sasl != nil {
		mechanism := strings.ToUpper(string(sasl.Mechanism))
		switch redpandav1alpha2.SASLMechanism(mechanism) {
		case redpandav1alpha2.SASLMechanismPlain, redpandav1alpha2.SASLMechanismScramSHA256, redpandav1alpha2.SASLMechanismScramSHA512:
		default:
			return nil, errors.Newf("cluster.staticConfiguration.kafka.sasl.mechanism %q is not supported for pipelines; use PLAIN, SCRAM-SHA-256, or SCRAM-SHA-512 (or configure sasl inline in configYaml)", sasl.Mechanism)
		}
		if sasl.Username == "" {
			return nil, errors.New("cluster.staticConfiguration.kafka.sasl.username must not be empty")
		}

		conn.SASL = &clusterSASL{Mechanism: mechanism, Username: sasl.Username}
		switch {
		case sasl.Password != nil && sasl.Password.SecretKeyRef != nil:
			conn.SASL.PasswordRef = sasl.Password.SecretKeyRef
		case sasl.Password != nil && sasl.Password.ConfigMapKeyRef != nil:
			conn.SASL.PasswordConfigMapRef = sasl.Password.ConfigMapKeyRef
		case sasl.Password != nil && sasl.Password.Inline != nil:
			conn.SASL.PasswordValue = *sasl.Password.Inline
		case sasl.Password != nil:
			return nil, errors.New("cluster.staticConfiguration.kafka.sasl.password must use inline, secretKeyRef, or configMapKeyRef")
		case sasl.DeprecatedPassword != nil: //nolint:staticcheck // deprecated field still accepted for spec compatibility
			if sasl.DeprecatedPassword.Key == "" { //nolint:staticcheck // ditto
				return nil, errors.New("cluster.staticConfiguration.kafka.sasl.passwordSecretRef.key must not be empty")
			}
			conn.SASL.PasswordRef = &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: sasl.DeprecatedPassword.Name}, //nolint:staticcheck // ditto
				Key:                  sasl.DeprecatedPassword.Key,                                     //nolint:staticcheck // ditto
			}
		default:
			return nil, errors.New("cluster.staticConfiguration.kafka.sasl.password is required when sasl is configured")
		}
	}

	return conn, nil
}

// staticTLSEnabled implements the CommonTLS contract (see the field docs on
// [redpandav1alpha2.CommonTLS]): TLS is used when explicitly enabled OR any
// certificate material / TLS-shaped option is specified. `tls: {enabled:
// false}` with no other fields means "connect without TLS" — previously any
// non-nil tls block forced TLS on, so a pipeline pointed at plaintext brokers
// with an explicit enabled: false attempted TLS and failed.
func staticTLSEnabled(tls *redpandav1alpha2.CommonTLS) bool {
	return tls.Enabled ||
		tls.InsecureSkipTLSVerify ||
		tls.CaCert != nil || tls.Cert != nil || tls.Key != nil ||
		tls.DeprecatedCaCert != nil || tls.DeprecatedCert != nil || tls.DeprecatedKey != nil //nolint:staticcheck // deprecated fields still accepted for spec compatibility
}

// staticClientCert maps the staticConfiguration's client cert/key (mTLS) onto
// the clusterTLS. The certificate may come from a Secret or ConfigMap; the
// private key must come from a Secret. Both halves must be set together.
func staticClientCert(tls *redpandav1alpha2.CommonTLS, out *clusterTLS) error {
	cert, key := tls.Cert, tls.Key

	// Fall back to the deprecated Secret-shaped fields when the typed ones
	// are absent, defaulting keys the same way the deprecated CA path does.
	if cert == nil && tls.DeprecatedCert != nil { //nolint:staticcheck // deprecated field still accepted for spec compatibility
		k := tls.DeprecatedCert.Key //nolint:staticcheck // ditto
		if k == "" {
			k = "tls.crt"
		}
		cert = &redpandav1alpha2.ValueSource{SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: tls.DeprecatedCert.Name}, //nolint:staticcheck // ditto
			Key:                  k,
		}}
	}
	if key == nil && tls.DeprecatedKey != nil { //nolint:staticcheck // deprecated field still accepted for spec compatibility
		k := tls.DeprecatedKey.Key //nolint:staticcheck // ditto
		if k == "" {
			k = "tls.key"
		}
		key = &redpandav1alpha2.ValueSource{SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: tls.DeprecatedKey.Name}, //nolint:staticcheck // ditto
			Key:                  k,
		}}
	}

	if cert == nil && key == nil {
		return nil
	}
	if (cert == nil) != (key == nil) {
		return errors.New("cluster.staticConfiguration.kafka.tls: cert and key must be set together for mTLS")
	}

	switch {
	case cert.SecretKeyRef != nil:
		out.ClientCertSecretRef = cert.SecretKeyRef
	case cert.ConfigMapKeyRef != nil:
		out.ClientCertConfigMapRef = cert.ConfigMapKeyRef
	default:
		return errors.New("cluster.staticConfiguration.kafka.tls.cert must use secretKeyRef or configMapKeyRef; store the client certificate in a Secret or ConfigMap")
	}

	if key.SecretKeyRef == nil {
		return errors.New("cluster.staticConfiguration.kafka.tls.key must use secretKeyRef; store the client private key in a Secret")
	}
	out.ClientKeySecretRef = key.SecretKeyRef

	return nil
}
