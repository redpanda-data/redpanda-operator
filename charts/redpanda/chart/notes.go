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
	return out
}

func cpuWarning(state *RenderState) string {
	coresInMillis := state.Values.Resources.CPU.Cores.MilliValue()
	if coresInMillis < 1000 {
		return fmt.Sprintf("%dm is below the minimum recommended CPU value for Redpanda", coresInMillis)
	}
	return ""
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
			// Yes, this really is a jsonpath string to be exposed to the user
			fmt.Sprintf(`  helm upgrade %s redpanda/redpanda --reuse-values -n %s --set $(kubectl get svc -n %s -o jsonpath='{"external.addresses={"}{ range .items[*]}{.status.loadBalancer.ingress[0].ip }{.status.loadBalancer.ingress[0].hostname}{","}{ end }{"}\n"}')`,
				Name(state),
				state.Release.Namespace,
				state.Release.Namespace,
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
		out = append(out,
			fmt.Sprintf(`  kubectl get secret -n %s %s-%s-cert -o go-template='{{ index .data "ca.crt" | base64decode }}' > ca.crt`,
				state.Release.Namespace,
				Fullname(state),
				external,
			),
		)
		if state.Values.Listeners.Kafka.TLS.RequireClientAuth || state.Values.Listeners.Admin.TLS.RequireClientAuth {
			out = append(out,
				fmt.Sprintf(`  kubectl get secret -n %s %s-client -o go-template='{{ index .data "tls.crt" | base64decode }}' > tls.crt`,
					state.Release.Namespace,
					Fullname(state),
				),
				fmt.Sprintf(`  kubectl get secret -n %s %s-client -o go-template='{{ index .data "tls.key" | base64decode }}' > tls.key`,
					state.Release.Namespace,
					Fullname(state),
				),
			)
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
			fmt.Sprintf(`  kubectl -n %s get secret %s -o go-template="{{ range .data }}{{ . | base64decode }}{{ end }}" | IFS=: read -r %s`,
				state.Release.Namespace,
				state.Values.Auth.SASL.SecretRef,
				rpkSASLEnvironmentVariables,
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
