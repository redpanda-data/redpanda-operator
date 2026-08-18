// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package checks

import (
	"context"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DeploymentCheck finds the operator Deployment and validates its TLS flag
// configuration. Populates cc.Deployment and cc.DeployArgs for downstream checks.
type DeploymentCheck struct{}

func (c *DeploymentCheck) Name() string { return "deployment" }

func (c *DeploymentCheck) Run(ctx context.Context, cc *CheckContext) []Result {
	var deploys appsv1.DeploymentList
	if err := cc.Ctl.List(ctx, cc.Namespace, &deploys, client.MatchingLabels{
		"app.kubernetes.io/name": cc.ServiceName,
	}); err != nil {
		return []Result{Fail(c.Name(), fmt.Sprintf("listing deployments: %v", err))}
	}
	if len(deploys.Items) == 0 {
		return []Result{Fail(c.Name(), fmt.Sprintf("no deployments found with label app.kubernetes.io/name=%s in namespace %s", cc.ServiceName, cc.Namespace))}
	}

	deploy := &deploys.Items[0]
	cc.Deployment = deploy

	// Extract container args.
	for _, cont := range deploy.Spec.Template.Spec.Containers {
		if len(cont.Args) > 0 {
			cc.DeployArgs = cont.Args
			break
		}
	}

	var results []Result

	// Validate TLS flag paths.
	expected := map[string]string{
		"--ca-file":          "/tls/ca.crt",
		"--certificate-file": "/tls/tls.crt",
		"--private-key-file": "/tls/tls.key",
	}
	for flag, want := range expected {
		got := ExtractFlag(cc.DeployArgs, flag)
		if got != want {
			results = append(results, Fail(c.Name(), fmt.Sprintf("Deployment %s=%s, expected %s", flag, got, want)))
		}
	}

	if len(results) == 0 {
		results = append(results, Pass(c.Name(), fmt.Sprintf("Deployment %s configured correctly", deploy.Name)))
	}
	return results
}

// ExtractFlag extracts the value of a --flag=value from args.
func ExtractFlag(args []string, flag string) string {
	prefix := flag + "="
	for _, arg := range args {
		if strings.HasPrefix(arg, prefix) {
			return strings.TrimPrefix(arg, prefix)
		}
	}
	return ""
}

// ExtractFlagAll extracts all values of a repeated --flag=value from args.
func ExtractFlagAll(args []string, flag string) []string {
	prefix := flag + "="
	var values []string
	for _, arg := range args {
		if strings.HasPrefix(arg, prefix) {
			values = append(values, strings.TrimPrefix(arg, prefix))
		}
	}
	return values
}
