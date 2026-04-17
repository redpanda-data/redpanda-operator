// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package cluster

import (
	"context"
	"fmt"

	"github.com/redpanda-data/common-go/kube"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	bootstrapUserSecretSuffix = "-bootstrap-user"
	bootstrapUserPasswordKey  = "password"
	caSecretSuffix            = "-root-certificate"
	ownerLabel                = "cluster.redpanda.com/owner"
)

// SecretsCheck finds the bootstrap user secret and CA secrets for the
// StretchCluster. Populates cc.BootstrapSecret and cc.CASecrets for
// cross-cluster consistency checks. Requires ResourceCheck to have run.
type SecretsCheck struct{}

func (c *SecretsCheck) Name() string { return "secrets" }

func (c *SecretsCheck) Run(ctx context.Context, cc *CheckContext) []Result {
	if cc.StretchCluster == nil {
		return []Result{Skip(c.Name(), "StretchCluster not available")}
	}

	var results []Result
	scName := cc.StretchCluster.Name

	// Check bootstrap user secret.
	bootstrapName := scName + bootstrapUserSecretSuffix
	bootstrapSecret := &corev1.Secret{}
	if err := cc.Ctl.Get(ctx, kube.ObjectKey{Namespace: cc.Namespace, Name: bootstrapName}, bootstrapSecret); err != nil {
		results = append(results, Fail(c.Name(), fmt.Sprintf("bootstrap user secret %s not found: %v", bootstrapName, err)))
	} else {
		cc.BootstrapSecret = bootstrapSecret
		if _, ok := bootstrapSecret.Data[bootstrapUserPasswordKey]; !ok {
			results = append(results, Fail(c.Name(), fmt.Sprintf("bootstrap user secret %s missing %q key", bootstrapName, bootstrapUserPasswordKey)))
		} else {
			results = append(results, Pass(c.Name(), fmt.Sprintf("bootstrap user secret %s exists", bootstrapName)))
		}
	}

	// Find CA secrets by owner label.
	var secrets corev1.SecretList
	if err := cc.Ctl.List(ctx, cc.Namespace, &secrets, client.MatchingLabels{
		ownerLabel: scName,
	}); err != nil {
		results = append(results, Fail(c.Name(), fmt.Sprintf("listing secrets: %v", err)))
		return results
	}

	for i := range secrets.Items {
		s := &secrets.Items[i]
		if s.Type == corev1.SecretTypeTLS {
			cc.CASecrets = append(cc.CASecrets, s)
		}
	}

	if len(cc.CASecrets) > 0 {
		results = append(results, Pass(c.Name(), fmt.Sprintf("%d CA secret(s) found", len(cc.CASecrets))))
	} else if cc.StretchCluster.Spec.TLS != nil {
		results = append(results, Fail(c.Name(), "TLS is configured but no CA secrets found"))
	}

	return results
}
