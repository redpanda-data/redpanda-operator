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

	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/common-go/kube"
	corev1 "k8s.io/api/core/v1"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2/conversion"
)

// clusterConnection holds the resolved connection details for a Redpanda cluster.
type clusterConnection struct {
	// Brokers is the list of internal Kafka broker addresses (host:port).
	Brokers []string
	// TLS holds TLS configuration if the cluster has TLS enabled.
	TLS *clusterTLS
	// SASL holds SASL credentials if the cluster has authentication enabled.
	SASL *clusterSASL
}

// clusterTLS holds TLS configuration resolved from a Redpanda cluster.
type clusterTLS struct {
	// CACertSecretRef points to the Secret and key containing the CA certificate.
	CACertSecretRef *corev1.SecretKeySelector
}

// clusterSASL holds the cluster's bootstrap user SASL credentials.
type clusterSASL struct {
	Mechanism   string
	Username    string
	PasswordRef *corev1.SecretKeySelector
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

	mechanism := "SCRAM-SHA-512"
	if t := user.Spec.Authentication.Type; t != nil && *t != "" {
		mechanism = strings.ToUpper(string(*t))
	}

	return &userCredentials{
		Mechanism:   mechanism,
		Username:    user.Name,
		PasswordRef: user.Spec.Authentication.Password.ValueFrom.SecretKeyRef,
	}, nil
}

// resolveClusterSource resolves the Pipeline's clusterRef to connection details.
// Returns nil if no clusterRef is set.
func resolveClusterSource(ctx context.Context, ctl *kube.Ctl, pipeline *redpandav1alpha2.Pipeline) (*clusterConnection, error) {
	if pipeline.Spec.ClusterSource == nil || pipeline.Spec.ClusterSource.ClusterRef == nil {
		return nil, nil
	}

	ref := pipeline.Spec.ClusterSource.ClusterRef

	var rp redpandav1alpha2.Redpanda
	if err := ctl.Get(ctx, kube.ObjectKey{Name: ref.Name, Namespace: pipeline.Namespace}, &rp); err != nil {
		return nil, errors.Wrapf(err, "failed to resolve clusterRef %q", ref.Name)
	}

	// Convert the Redpanda CR to a RenderState, then extract connection details.
	// This is the same pattern used by the Console controller.
	state, err := conversion.ConvertV2ToRenderState(nil, &conversion.V2Defaulters{
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

		if cfg.Kafka.TLS != nil && cfg.Kafka.TLS.CaCert != nil && cfg.Kafka.TLS.CaCert.SecretKeyRef != nil {
			conn.TLS = &clusterTLS{
				CACertSecretRef: cfg.Kafka.TLS.CaCert.SecretKeyRef,
			}
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

	return conn, nil
}
