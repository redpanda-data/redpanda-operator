// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package kube

import (
	"context"
	"maps"
	"os"

	"github.com/cockroachdb/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type EnvExpander struct {
	Client    client.Client
	Namespace string
	Env       []corev1.EnvVar
	EnvFrom   []corev1.EnvFromSource
}

// Expand expands the given string as if [os.ExpandEnv] was run on it
// within a Pod having the env and envFrom of this EnvExpander.
func (t *EnvExpander) Expand(ctx context.Context, s string) (string, error) {
	vars := map[string]string{}
	for _, src := range t.EnvFrom {
		resolved, err := t.resolve(ctx, src)
		if err != nil {
			return "", err
		}
		maps.Copy(vars, resolved)
	}

	var errs []error
	expanded := os.Expand(s, func(s string) string {
		for _, e := range t.Env {
			if e.Name == s {
				value, err := t.valueOf(ctx, e)
				if err != nil {
					errs = append(errs, err)
					return ""
				}
				return value
			}
		}
		return vars[s]
	})

	if len(errs) > 0 {
		return "", errors.Join(errs...)
	}

	return expanded, nil
}

func (t *EnvExpander) resolve(ctx context.Context, src corev1.EnvFromSource) (map[string]string, error) {
	if src.SecretRef != nil {
		ref := src.SecretRef
		key := client.ObjectKey{Namespace: t.Namespace, Name: ref.Name}

		var secret corev1.Secret
		if err := t.Client.Get(ctx, key, &secret); err != nil {
			if apierrors.IsNotFound(err) && ptr.Deref(ref.Optional, false) {
				return map[string]string{}, nil
			}
		}

		ret := make(map[string]string, len(secret.Data))
		for k, v := range secret.Data {
			ret[src.Prefix+k] = string(v)
		}
		return ret, nil
	}

	if src.ConfigMapRef != nil {
		ref := src.ConfigMapRef
		key := client.ObjectKey{Namespace: t.Namespace, Name: ref.Name}

		var cm corev1.ConfigMap
		if err := t.Client.Get(ctx, key, &cm); err != nil {
			if apierrors.IsNotFound(err) && ptr.Deref(ref.Optional, false) {
				return map[string]string{}, nil
			}
		}

		ret := make(map[string]string, len(cm.Data))
		for k, v := range cm.Data {
			ret[src.Prefix+k] = string(v)
		}
		return ret, nil
	}

	return nil, errors.Newf("invalid %T: %#v", src, src)
}

func (t *EnvExpander) valueOf(ctx context.Context, e corev1.EnvVar) (string, error) {
	if e.Value != "" {
		return e.Value, nil
	}

	if vf := e.ValueFrom; vf != nil {
		switch {
		case vf.ConfigMapKeyRef != nil:
			return t.resolveConfigMapRef(ctx, vf.ConfigMapKeyRef)

		case vf.SecretKeyRef != nil:
			return t.resolveSecretRef(ctx, vf.SecretKeyRef)

		default:
			return "", errors.Newf("not supported: %#v", vf)
		}
	}

	return "", errors.Newf("invalid %T: %#v", e, e)
}

func (t *EnvExpander) resolveConfigMapRef(ctx context.Context, ref *corev1.ConfigMapKeySelector) (string, error) {
	key := client.ObjectKey{Namespace: t.Namespace, Name: ref.Name}

	var cm corev1.ConfigMap
	if err := t.Client.Get(ctx, key, &cm); err != nil {
		if apierrors.IsNotFound(err) && ptr.Deref(ref.Optional, false) {
			return "", nil
		}
		return "", err
	}

	if value, ok := cm.Data[ref.Key]; ok {
		return value, nil
	}

	if ptr.Deref(ref.Optional, false) {
		return "", nil
	}

	return "", errors.Newf("missing key: %q", ref.Key)
}

func (t *EnvExpander) resolveSecretRef(ctx context.Context, ref *corev1.SecretKeySelector) (string, error) {
	key := client.ObjectKey{Namespace: t.Namespace, Name: ref.Name}

	var cm corev1.Secret
	if err := t.Client.Get(ctx, key, &cm); err != nil {
		if apierrors.IsNotFound(err) && ptr.Deref(ref.Optional, false) {
			return "", nil
		}
		return "", err
	}

	if value, ok := cm.Data[ref.Key]; ok {
		return string(value), nil
	}

	if ptr.Deref(ref.Optional, false) {
		return "", nil
	}

	return "", errors.Newf("missing key: %q", ref.Key)
}
