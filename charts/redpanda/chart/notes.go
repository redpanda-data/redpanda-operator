// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_notes.go.tpl
package chart

import (
	"fmt"
	"strings"

	"golang.org/x/exp/maps"
	corev1 "k8s.io/api/core/v1"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

// rpkSASLEnvironmentVariables is the set of environment variables rpk reads
// SASL credentials from. The REDPANDA_SASL_* spelling it replaced was only
// needed for Redpanda older than v23.2.1.
//
// was:   rpk sasl environment variables
const rpkSASLEnvironmentVariables = `RPK_USER RPK_PASS RPK_SASL_MECHANISM`

// communityCoreLimit is the most cores, summed across a cluster's brokers,
// that Redpanda v26.3 and later run without an Enterprise license.
const communityCoreLimit = 50

// Notes is the entrypoint for NOTES.txt, which is rendered outside of
// [render] and therefore has no [RenderState] of its own.
func Notes(dot *helmette.Dot) []string {
	// NB: Constructed inline for the same reason as in [render]; returning a
	// *RenderState would jsonify it.
	state := &RenderState{
		Release: &dot.Release,
		Files:   &dot.Files,
		Chart:   &dot.Chart,
		Values:  helmette.Unwrap[Values](dot.Values),
		Dot:     dot,
	}

	return append(warnings(state), notes(state)...)
}

func warnings(state *RenderState) []string {
	var out []string
	if w := cpuWarning(state); w != "" {
		out = append(out, fmt.Sprintf(`**Warning**: %s`, w))
	}
	if w := coreLimitWarning(state); w != "" {
		out = append(out, fmt.Sprintf(`**Warning**: %s`, w))
	}
	return out
}

func cpuWarning(state *RenderState) string {
	coresInMillis := state.Values.Resources.CPU.Cores.MilliValue()
	if coresInMillis < 1000 {
		return fmt.Sprintf("%dm is below the minimum recommended CPU value for Redpanda", coresInMillis)
	}
	return ""
}

// coreLimitWarning explains the Community Edition core limit when this
// release's brokers may exceed it and no license is configured. A license set
// with rpk instead, or brokers outside this release, can't be seen here.
func coreLimitWarning(state *RenderState) string {
	if state.Values.Enterprise.License != "" || state.Values.Enterprise.LicenseSecretRef != nil {
		return ""
	}
	smp := brokerSMP(state)
	replicas := int64(state.Values.Statefulset.Replicas)
	total := smp * replicas
	var usage string
	if smp == 0 {
		usage = `This release sets no CPU for its brokers, so each uses every core on its node.`
	} else if total > communityCoreLimit {
		usage = fmt.Sprintf(`This release runs %d cores (%d brokers x %d).`, total, replicas, smp)
	} else {
		return ""
	}
	return fmt.Sprintf(
		`%s Redpanda v26.3+ limits clusters without an Enterprise license to %d cores in total; over it, a broker upgrading across a major version (e.g. v26.2 to v26.3) refuses to start and logs cluster_cores_over_limit. To fix, set enterprise.license (or licenseSecretRef) or run at most %d cores. See https://docs.redpanda.com/current/get-started/licenses.`,
		usage, communityCoreLimit, communityCoreLimit,
	)
}

// brokerSMP is the --smp each broker starts with, or 0 when none is set and
// Redpanda takes every core on its node.
func brokerSMP(state *RenderState) int64 {
	_, _, flags := RedpandaAdditionalStartFlags(&state.Values, Pool{Statefulset: state.Values.Statefulset})
	for _, flag := range flags {
		if strings.HasPrefix(flag, "--smp=") {
			smp, _ := helmette.Atoi(strings.TrimPrefix(flag, "--smp="))
			return int64(smp)
		}
	}
	return 0
}

func notes(state *RenderState) []string {
	anySASL := state.Values.Auth.IsSASLEnabled()
	var out []string
	out = append(out,
		``, ``, ``, ``,
		fmt.Sprintf(`Congratulations on installing %s!`, state.Chart.Name),
		``,
		`The pods will rollout in a few seconds. To check the status:`,
		``,
		fmt.Sprintf(`  kubectl -n %s rollout status statefulset %s --watch`,
			state.Release.Namespace,
			Fullname(state),
		),
	)
	if state.Values.External.Enabled && state.Values.External.Type == corev1.ServiceTypeLoadBalancer {
		out = append(out,
			``,
			`If you are using the load balancer service with a cloud provider, the services will likely have automatically-generated addresses. In this scenario the advertised listeners must be updated in order for external access to work. Run the following command once Redpanda is deployed:`,
			``,
			// Yes, this really is a jsonpath string to be exposed to the user.
			fmt.Sprintf(`  helm upgrade %s redpanda/redpanda --reuse-values -n %s --set $(kubectl get svc -n %s -l app.kubernetes.io/instance=%s,%s=%s -o jsonpath='{"external.addresses={"}{ range .items[*]}{.status.loadBalancer.ingress[0].ip }{.status.loadBalancer.ingress[0].hostname}{","}{ end }{"}\n"}')`,
				state.Release.Name,
				state.Release.Namespace,
				state.Release.Namespace,
				state.Release.Name,
				loadBalancerTypeLabelKey,
				loadBalancerTypeLabelValue,
			),
		)
	}
	profiles := maps.Keys(state.Values.Listeners.Kafka.External)
	profiles = helmette.SortAlpha(profiles)
	profileName := profiles[0]
	out = append(out,
		``,
		`Set up rpk for access to your external listeners:`,
	)
	profile := state.Values.Listeners.Kafka.External[profileName]
	if profile.TLS.IsEnabled(&state.Values.Listeners.Kafka.TLS, &state.Values.TLS) {
		var external string
		if profile.TLS != nil && profile.TLS.Cert != nil {
			external = *profile.TLS.Cert
		} else {
			external = state.Values.Listeners.Kafka.TLS.Cert
		}
		serverCert := state.Values.TLS.Certs.MustGet(external)
		out = append(out,
			fmt.Sprintf(`  kubectl get secret -n %s %s -o go-template='{{ index .data "ca.crt" | base64decode }}' > ca.crt`,
				state.Release.Namespace,
				serverCert.ServerSecretName(state, external),
			),
		)
		// One fetch per listener requiring client auth: rpk presents the
		// kafka listener's client cert on the kafka API and the admin
		// listener's on the admin API (see rpk*ClientTLSConfiguration).
		var kafkaClientSecret string
		if state.Values.Listeners.Kafka.TLS.RequireClientAuth {
			certName := state.Values.Listeners.Kafka.TLS.Cert
			cert := state.Values.TLS.Certs.MustGet(certName)
			kafkaClientSecret = cert.ClientSecretName(state, certName)
			out = append(out,
				fmt.Sprintf(`  kubectl get secret -n %s %s -o go-template='{{ index .data "tls.crt" | base64decode }}' > tls.crt`,
					state.Release.Namespace,
					kafkaClientSecret,
				),
				fmt.Sprintf(`  kubectl get secret -n %s %s -o go-template='{{ index .data "tls.key" | base64decode }}' > tls.key`,
					state.Release.Namespace,
					kafkaClientSecret,
				),
			)
		}
		if state.Values.Listeners.Admin.TLS.RequireClientAuth {
			certName := state.Values.Listeners.Admin.TLS.Cert
			cert := state.Values.TLS.Certs.MustGet(certName)
			clientSecretName := cert.ClientSecretName(state, certName)
			// Both APIs commonly share one client cert; the kafka fetch
			// above already wrote it.
			if clientSecretName != kafkaClientSecret {
				out = append(out,
					fmt.Sprintf(`  kubectl get secret -n %s %s -o go-template='{{ index .data "tls.crt" | base64decode }}' > admin-tls.crt`,
						state.Release.Namespace,
						clientSecretName,
					),
					fmt.Sprintf(`  kubectl get secret -n %s %s -o go-template='{{ index .data "tls.key" | base64decode }}' > admin-tls.key`,
						state.Release.Namespace,
						clientSecretName,
					),
				)
			}
		}
	}
	out = append(out,
		fmt.Sprintf(`  rpk profile create --from-profile <(kubectl get configmap -n %s %s-rpk -o go-template='{{ .data.profile }}') %s`,
			state.Release.Namespace,
			Fullname(state),
			profileName,
		),
		``,
		`Set up dns to look up the pods on their Kubernetes Nodes. You can use this query to get the list of short-names to IP addresses. Add your external domain to the hostnames and you could test by adding these to your /etc/hosts:`,
		``,
		fmt.Sprintf(`  kubectl get pod -n %s -o custom-columns=node:.status.hostIP,name:.metadata.name --no-headers -l app.kubernetes.io/name=redpanda,app.kubernetes.io/component=redpanda-statefulset`,
			state.Release.Namespace,
		),
	)
	if anySASL {
		out = append(out,
			``,
			`Set the credentials in the environment:`,
			``,
			// In bash, `read` at the end of a pipeline runs in a subshell and
			// its variables vanish; process substitution keeps them in the
			// user's shell.
			fmt.Sprintf(`  IFS=: read -r %s < <(kubectl -n %s get secret %s -o go-template="{{ range .data }}{{ . | base64decode }}{{ end }}")`,
				rpkSASLEnvironmentVariables,
				state.Release.Namespace,
				state.Values.Auth.SASL.SecretRef,
			),
			fmt.Sprintf(`  export %s`,
				rpkSASLEnvironmentVariables,
			),
		)
	}
	out = append(out,
		``,
		`Try some sample commands:`,
	)
	if anySASL {
		out = append(out,
			`Create a user:`,
			``,
			fmt.Sprintf(`  rpk acl user create myuser --new-password changeme --mechanism %s`, state.Values.Auth.SASL.GetMechanism()),
			``,
			`Give the user permissions:`,
			``,
			`  rpk acl create --allow-principal 'myuser' --allow-host '*' --operation all --topic 'test-topic'`,
		)
	}
	out = append(out,
		``,
		`Get the api status:`,
		``,
		`  rpk cluster info`,
		``,
		`Create a topic`,
		``,
		fmt.Sprintf(`  rpk topic create test-topic -p 3 -r %d`, helmette.Min(3, int64(state.Values.Statefulset.Replicas))),
		``,
		`Describe the topic:`,
		``,
		`  rpk topic describe test-topic`,
		``,
		`Delete the topic:`,
		``,
		`  rpk topic delete test-topic`,
	)

	return out
}
