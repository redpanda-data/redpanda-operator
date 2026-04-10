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

// clusterSASL holds SASL credentials resolved from a Redpanda cluster.
type clusterSASL struct {
	Mechanism string
	Username  string
	// Password is the env var to inject for the SASL password. Built from
	// the appropriate ValueSource (Secret, ConfigMap, inline, or resolved
	// external secret).
	Password corev1.EnvVar
	// FromCredentials is true when the credentials come from the Pipeline's
	// spec.credentials field (dedicated user) rather than the cluster's
	// bootstrap admin user. This controls which env var names are used.
	FromCredentials bool
}

// BrokersString returns the broker list as a comma-separated string.
func (c *clusterConnection) BrokersString() string {
	return strings.Join(c.Brokers, ",")
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

		// Use explicit Pipeline credentials if provided; otherwise fall back
		// to the cluster's bootstrap (admin) user.
		if creds := pipeline.Spec.Credentials; creds != nil {
			conn.SASL = &clusterSASL{
				Mechanism:       creds.Mechanism,
				Username:        creds.Username,
				Password:        envVarFromValueSource("RPK_CREDENTIALS_SASL_PASSWORD", &creds.Password),
				FromCredentials: true,
			}
		} else if cfg.Kafka.SASL != nil {
			sasl := &clusterSASL{
				Mechanism: string(cfg.Kafka.SASL.Mechanism),
				Username:  cfg.Kafka.SASL.Username,
			}
			if cfg.Kafka.SASL.Password != nil && cfg.Kafka.SASL.Password.SecretKeyRef != nil {
				sasl.Password = corev1.EnvVar{
					Name: "RPK_SASL_PASSWORD",
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: cfg.Kafka.SASL.Password.SecretKeyRef,
					},
				}
			}
			conn.SASL = sasl
		}
	}

	return conn, nil
}

// envVarFromValueSource converts a ValueSource into a corev1.EnvVar. Supports
// SecretKeyRef, ConfigMapKeyRef, and Inline sources. ExternalSecretRef is not
// supported in this path — use ESO or the operator's cloud secret expander to
// sync external secrets into a Kubernetes Secret first.
func envVarFromValueSource(name string, vs *redpandav1alpha2.ValueSource) corev1.EnvVar {
	if vs == nil {
		return corev1.EnvVar{Name: name}
	}

	if vs.SecretKeyRef != nil {
		return corev1.EnvVar{
			Name: name,
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: vs.SecretKeyRef,
			},
		}
	}

	if vs.ConfigMapKeyRef != nil {
		return corev1.EnvVar{
			Name: name,
			ValueFrom: &corev1.EnvVarSource{
				ConfigMapKeyRef: vs.ConfigMapKeyRef,
			},
		}
	}

	if vs.Inline != nil {
		return corev1.EnvVar{
			Name:  name,
			Value: *vs.Inline,
		}
	}

	return corev1.EnvVar{Name: name}
}
