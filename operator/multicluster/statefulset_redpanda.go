// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package multicluster

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// statefulSetRedpandaEnv returns the environment variables shared by
// the Redpanda and sidecar containers.
func statefulSetRedpandaEnv() []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name: "SERVICE_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.name",
				},
			},
		},
		{
			Name: "POD_IP",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "status.podIP",
				},
			},
		},
		{
			Name: "HOST_IP",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "status.hostIP",
				},
			},
		},
		{
			Name: "ORDINAL_NUMBER",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.labels['apps.kubernetes.io/pod-index']",
				},
			},
		},
	}
}

func statefulSetContainerRedpanda(state *RenderState, pool *redpandav1alpha2.NodePool) corev1.Container {
	internalAdvertiseAddress := fmt.Sprintf("%s-%s.%s", pool.GetName(), "$(ORDINAL_NUMBER)", state.namespace)

	terminationGracePeriod := defaultTerminationGracePeriod

	p := scriptParamsFromState(state)

	l := state.Spec().Listeners

	ports := redpandaContainerPorts(l, state.Spec())

	// Get resource requirements from the spec.
	resources := state.Spec().GetResourceRequirements()

	env := statefulSetRedpandaEnv()
	env = append(env, metricsEnvironmentVariables(state, pool)...)

	container := corev1.Container{
		Name:      redpandaContainerName,
		Image:     pool.RedpandaImage(),
		Env:       env,
		Resources: resources,
		// Lifecycle hooks manage maintenance mode transitions:
		//   postStart: clears maintenance mode so the broker rejoins the cluster.
		//   preStop: enables maintenance mode to drain leadership before shutdown.
		// Timeout is half the termination grace period, leaving the other half
		// for Redpanda to shut down cleanly after the preStop hook completes.
		Lifecycle: &corev1.Lifecycle{
			PostStart: &corev1.LifecycleHandler{
				Exec: &corev1.ExecAction{
					Command: wrapLifecycleHook("post-start", terminationGracePeriod/2,
						[]string{"bash", "-x", "/var/lifecycle/postStart.sh"}),
				},
			},
			PreStop: &corev1.LifecycleHandler{
				Exec: &corev1.ExecAction{
					Command: wrapLifecycleHook("pre-stop", terminationGracePeriod/2,
						[]string{"bash", "-x", "/var/lifecycle/preStop.sh"}),
				},
			},
		},
		// Startup probe: generous threshold (120 failures × 10s = 20 min) to
		// accommodate initial cluster bootstrap and data recovery on restart.
		// Once the startup probe succeeds, the liveness probe takes over.
		StartupProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				Exec: &corev1.ExecAction{
					Command: []string{`/bin/sh`, `-c`, startupProbeScript(p)},
				},
			},
			FailureThreshold:    120,
			InitialDelaySeconds: 1,
			PeriodSeconds:       10,
		},
		// Liveness probe: tight threshold (3 failures = 30s) to quickly
		// restart a broker that becomes unresponsive after startup.
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				Exec: &corev1.ExecAction{
					Command: []string{`/bin/sh`, `-c`, livenessProbeScript(p)},
				},
			},
			FailureThreshold:    3,
			InitialDelaySeconds: 10,
			PeriodSeconds:       10,
		},
		Command: []string{
			`rpk`, `redpanda`, `start`,
			fmt.Sprintf(`--advertise-rpc-addr=%s:%d`, internalAdvertiseAddress, p.RPCPort),
		},
		Ports:        ports,
		VolumeMounts: statefulSetVolumeMounts(state),
	}

	return container
}

// redpandaContainerPorts returns the container ports for the Redpanda container,
// including internal ports and any enabled external listener ports.
func redpandaContainerPorts(l *redpandav1alpha2.StretchListeners, spec *redpandav1alpha2.StretchClusterSpec) []corev1.ContainerPort {
	var ports []corev1.ContainerPort

	// Internal ports + their external listeners.
	type listenerPorts struct {
		internalName string
		internalPort int32
		extPrefix    string
		defaultPort  int32
		externals    map[string]*redpandav1alpha2.StretchExternalListener
	}

	listeners := []listenerPorts{
		{internalAdminAPIPortName, spec.AdminPort(), "admin", redpandav1alpha2.DefaultExternalAdminPort, nil},
		{internalPandaProxyPortName, spec.HTTPPort(), "http", redpandav1alpha2.DefaultExternalHTTPPort, nil},
		{internalKafkaPortName, spec.KafkaPort(), "kafka", redpandav1alpha2.DefaultExternalKafkaPort, nil},
		{internalRPCPortName, spec.RPCPort(), "", 0, nil},
		{internalSchemaRegistryPortName, spec.SchemaRegistryPort(), "schema", redpandav1alpha2.DefaultExternalSchemaRegistryPort, nil},
	}

	// Wire up external listeners from the spec.
	if l != nil {
		if l.Admin != nil {
			listeners[0].externals = l.Admin.External
		}
		if l.HTTP != nil {
			listeners[1].externals = l.HTTP.External
		}
		if l.Kafka != nil {
			listeners[2].externals = l.Kafka.External
		}
		if l.SchemaRegistry != nil {
			listeners[4].externals = l.SchemaRegistry.External
		}
	}

	for _, lp := range listeners {
		ports = append(ports, corev1.ContainerPort{
			Name: lp.internalName, ContainerPort: lp.internalPort,
		})
		if lp.extPrefix != "" {
			forEachEnabledExternal(lp.externals, func(name string, ext *redpandav1alpha2.StretchExternalListener) {
				ports = append(ports, corev1.ContainerPort{
					Name:          fmt.Sprintf("%s-%.8s", lp.extPrefix, name),
					ContainerPort: ext.GetPort(lp.defaultPort),
				})
			})
		}
	}

	return ports
}

// metricsEnvironmentVariables returns the REDPANDA_METRICS_K8S_* env vars for
// the Redpanda container if metrics reporting is enabled.
func metricsEnvironmentVariables(state *RenderState, pool *redpandav1alpha2.NodePool) []corev1.EnvVar {
	if !state.Spec().IsMetricsReporterEnabled() {
		return nil
	}

	return []corev1.EnvVar{
		{Name: "REDPANDA_METRICS_K8S_DEPLOYMENT_TYPE", Value: "operator"},
		{Name: "REDPANDA_METRICS_K8S_CHART_VERSION", Value: redpandav1alpha2.DefaultOperatorImageTag},
		{Name: "REDPANDA_METRICS_K8S_OPERATOR_IMAGE_VERSION", Value: pool.SidecarImage()},
	}
}
