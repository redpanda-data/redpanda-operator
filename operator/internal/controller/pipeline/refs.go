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
	"crypto/sha256"
	"fmt"
	"sort"

	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/common-go/kube"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/utils/ptr"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// validateValueSources checks that every spec.valueSources entry resolves to
// an existing backing object and key. Entries marked optional on their
// selector are allowed to be missing (matching the kubelet's env semantics).
// Returning an error here keeps the previously-running Deployment untouched
// instead of rolling into pods stuck in CreateContainerConfigError.
func validateValueSources(ctx context.Context, ctl *kube.Ctl, pipeline *redpandav1alpha2.Pipeline) error {
	for _, vs := range pipeline.Spec.ValueSources {
		switch {
		case vs.Source.Inline != nil:
			// Nothing to resolve.

		case vs.Source.SecretKeyRef != nil:
			ref := vs.Source.SecretKeyRef
			var secret corev1.Secret
			if err := ctl.Get(ctx, kube.ObjectKey{Name: ref.Name, Namespace: pipeline.Namespace}, &secret); err != nil {
				if apierrors.IsNotFound(err) && ptr.Deref(ref.Optional, false) {
					continue
				}
				return errors.Wrapf(err, "valueSources[%s]: failed to read Secret %q", vs.Name, ref.Name)
			}
			if _, ok := secret.Data[ref.Key]; !ok && !ptr.Deref(ref.Optional, false) {
				return errors.Newf("valueSources[%s]: Secret %q has no key %q", vs.Name, ref.Name, ref.Key)
			}

		case vs.Source.ConfigMapKeyRef != nil:
			ref := vs.Source.ConfigMapKeyRef
			var cm corev1.ConfigMap
			if err := ctl.Get(ctx, kube.ObjectKey{Name: ref.Name, Namespace: pipeline.Namespace}, &cm); err != nil {
				if apierrors.IsNotFound(err) && ptr.Deref(ref.Optional, false) {
					continue
				}
				return errors.Wrapf(err, "valueSources[%s]: failed to read ConfigMap %q", vs.Name, ref.Name)
			}
			if _, inData := cm.Data[ref.Key]; !inData {
				if _, inBinary := cm.BinaryData[ref.Key]; !inBinary && !ptr.Deref(ref.Optional, false) {
					return errors.Newf("valueSources[%s]: ConfigMap %q has no key %q", vs.Name, ref.Name, ref.Key)
				}
			}

		case vs.Source.ExternalSecretRefSelector != nil:
			// External secrets are materialized as a Kubernetes Secret by the
			// external-secrets operator; the env projection reads key <entry
			// name> from a Secret named after the ref (see
			// buildValueSourceEnv). Materialization can lag the Pipeline —
			// the reconcile requeues until it lands.
			name := vs.Source.ExternalSecretRefSelector.Name
			var secret corev1.Secret
			if err := ctl.Get(ctx, kube.ObjectKey{Name: name, Namespace: pipeline.Namespace}, &secret); err != nil {
				return errors.Wrapf(err, "valueSources[%s]: failed to read externally-managed Secret %q (has external-secrets materialized it yet?)", vs.Name, name)
			}
			if _, ok := secret.Data[vs.Name]; !ok {
				return errors.Newf("valueSources[%s]: externally-managed Secret %q has no key %q", vs.Name, name, vs.Name)
			}
		}
	}
	return nil
}

// credentialsChecksum digests every Secret/ConfigMap the pipeline pod
// consumes by reference — userRef password, cluster SASL credentials, TLS
// material, valueSources — plus the operator license bytes. The result is
// stamped onto the pod template (see render.credentialsChecksum) so rotating
// any referenced credential rolls the Deployment: env vars from
// secretKeyRef/configMapKeyRef and startup-read certificate files only take
// effect in new pods.
//
// The digest is built from object resourceVersions, not contents, so no
// content-derived value of a secret lands on the (widely readable) pod
// template. Objects that don't exist are skipped: their later appearance
// changes the digest, which is exactly the roll we want.
func (c *Controller) credentialsChecksum(ctx context.Context, pipeline *redpandav1alpha2.Pipeline, conn *clusterConnection, creds *userCredentials, licenseContent []byte) (string, error) {
	type ref struct {
		kind string // "Secret" or "ConfigMap"
		name string
	}
	refs := map[ref]struct{}{}

	addSecret := func(sel *corev1.SecretKeySelector) {
		if sel != nil {
			refs[ref{kind: "Secret", name: sel.Name}] = struct{}{}
		}
	}
	addConfigMap := func(sel *corev1.ConfigMapKeySelector) {
		if sel != nil {
			refs[ref{kind: "ConfigMap", name: sel.Name}] = struct{}{}
		}
	}

	for _, vs := range pipeline.Spec.ValueSources {
		addSecret(vs.Source.SecretKeyRef)
		addConfigMap(vs.Source.ConfigMapKeyRef)
		if es := vs.Source.ExternalSecretRefSelector; es != nil {
			refs[ref{kind: "Secret", name: es.Name}] = struct{}{}
		}
	}
	if creds != nil {
		addSecret(creds.PasswordRef)
	}
	if conn != nil {
		if sasl := conn.SASL; sasl != nil {
			addSecret(sasl.PasswordRef)
			addConfigMap(sasl.PasswordConfigMapRef)
		}
		if tls := conn.TLS; tls != nil {
			addSecret(tls.CACertSecretRef)
			addConfigMap(tls.CACertConfigMapRef)
			addSecret(tls.ClientCertSecretRef)
			addConfigMap(tls.ClientCertConfigMapRef)
			addSecret(tls.ClientKeySecretRef)
		}
	}

	var entries []string
	for r := range refs {
		var (
			rv  string
			err error
		)
		switch r.kind {
		case "Secret":
			var secret corev1.Secret
			err = c.Ctl.Get(ctx, kube.ObjectKey{Name: r.name, Namespace: pipeline.Namespace}, &secret)
			rv = secret.ResourceVersion
		case "ConfigMap":
			var cm corev1.ConfigMap
			err = c.Ctl.Get(ctx, kube.ObjectKey{Name: r.name, Namespace: pipeline.Namespace}, &cm)
			rv = cm.ResourceVersion
		}
		if err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return "", errors.Wrapf(err, "computing credentials checksum: reading %s %q", r.kind, r.name)
		}
		entries = append(entries, fmt.Sprintf("%s/%s=%s", r.kind, r.name, rv))
	}

	if len(licenseContent) > 0 {
		entries = append(entries, fmt.Sprintf("license=%x", sha256.Sum256(licenseContent)))
	}

	// An inline SASL password is mirrored into the Pipeline-owned SASL Secret
	// and referenced by a constant name/key, so a spec change to the inline
	// value must roll the pods through this digest instead. (The value is
	// already plaintext in the Pipeline spec, so a hash of it on the pod
	// template discloses nothing new.)
	if conn != nil && conn.SASL != nil && conn.SASL.PasswordValue != "" {
		entries = append(entries, fmt.Sprintf("inline-sasl=%x", sha256.Sum256([]byte(conn.SASL.PasswordValue))))
	}

	if len(entries) == 0 {
		return "", nil
	}

	sort.Strings(entries)
	h := sha256.New()
	for _, e := range entries {
		// sha256's Write never returns an error.
		_, _ = fmt.Fprintf(h, "%s\x00", e)
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
