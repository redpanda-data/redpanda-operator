// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_notes.go.tpl
package redpanda

import (
	"fmt"

	"golang.org/x/exp/maps"
	corev1 "k8s.io/api/core/v1"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

func Warnings(state *RenderState) []string {
	var warnings []string
	if w := cpuWarning(state); w != "" {
		warnings = append(warnings, fmt.Sprintf(`**Warning**: %s`, w))
	}
	return warnings
}

func cpuWarning(state *RenderState) string {
	coresInMillis := state.Values.Resources.CPU.Cores.MilliValue()
	if coresInMillis < 1000 {
		return fmt.Sprintf("%dm is below the minimum recommended CPU value for Redpanda", coresInMillis)
	}
	return ""
}

func Notes(state *RenderState) []string {
	anySASL := state.Values.Auth.IsSASLEnabled()
	var notes []string
	notes = append(notes,
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
		notes = append(notes,
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
	helmette.SortAlpha(profiles)
	profileName := profiles[0]
	notes = append(notes,
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
		notes = append(notes,
			fmt.Sprintf(`  kubectl get secret -n %s %s-%s-cert -o go-template='{{ index .data "ca.crt" | base64decode }}' > ca.crt`,
				state.Release.Namespace,
				Fullname(state),
				external,
			),
		)
		if state.Values.Listeners.Kafka.TLS.RequireClientAuth || state.Values.Listeners.Admin.TLS.RequireClientAuth {
			notes = append(notes,
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
	notes = append(notes,
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
		notes = append(notes,
			``,
			`Set the credentials in the environment:`,
			``,
			fmt.Sprintf(`  kubectl -n %s get secret %s -o go-template="{{ range .data }}{{ . | base64decode }}{{ end }}" | IFS=: read -r %s`,
				state.Release.Namespace,
				state.Values.Auth.SASL.SecretRef,
				RpkSASLEnvironmentVariables(state),
			),
			fmt.Sprintf(`  export %s`,
				RpkSASLEnvironmentVariables(state),
			),
		)
	}
	notes = append(notes,
		``,
		`Try some sample commands:`,
	)
	if anySASL {
		notes = append(notes,
			`Create a user:`,
			``,
			fmt.Sprintf(`  %s`, RpkACLUserCreate(state)),
			``,
			`Give the user permissions:`,
			``,
			fmt.Sprintf(`  %s`, RpkACLCreate(state)),
		)
	}
	notes = append(notes,
		``,
		`Get the api status:`,
		``,
		fmt.Sprintf(`  %s`, RpkClusterInfo(state)),
		``,
		`Create a topic`,
		``,
		fmt.Sprintf(`  %s`, RpkTopicCreate(state)),
		``,
		`Describe the topic:`,
		``,
		fmt.Sprintf(`  %s`, RpkTopicDescribe(state)),
		``,
		`Delete the topic:`,
		``,
		fmt.Sprintf(`  %s`, RpkTopicDelete(state)),
	)

	return notes
}

// Any rpk command that's given to the user in in this file must be defined in _example-commands.tpl and tested in a test.
// These are all tested in `tests/test-kafka-sasl-status.yaml`

func RpkACLUserCreate(state *RenderState) string {
	return fmt.Sprintf(`rpk acl user create myuser --new-password changeme --mechanism %s`, GetSASLMechanism(state))
}

func GetSASLMechanism(state *RenderState) SASLMechanism {
	if state.Values.Auth.SASL != nil {
		return state.Values.Auth.SASL.Mechanism
	}
	return "SCRAM-SHA-512"
}

func RpkACLCreate(*RenderState) string {
	return `rpk acl create --allow-principal 'myuser' --allow-host '*' --operation all --topic 'test-topic'`
}

func RpkClusterInfo(*RenderState) string {
	return `rpk cluster info`
}

func RpkTopicCreate(state *RenderState) string {
	return fmt.Sprintf(`rpk topic create test-topic -p 3 -r %d`, helmette.Min(3, int64(state.Values.Statefulset.Replicas)))
}

func RpkTopicDescribe(*RenderState) string {
	return `rpk topic describe test-topic`
}

func RpkTopicDelete(state *RenderState) string {
	return `rpk topic delete test-topic`
}

// was:   rpk sasl environment variables
//
// This will return a string with the correct environment variables to use for SASL based on the
// version of the redpanda container being used
func RpkSASLEnvironmentVariables(state *RenderState) string {
	if RedpandaAtLeast_23_2_1(state) {
		return `RPK_USER RPK_PASS RPK_SASL_MECHANISM`
	} else {
		return `REDPANDA_SASL_USERNAME REDPANDA_SASL_PASSWORD REDPANDA_SASL_MECHANISM`
	}
}
