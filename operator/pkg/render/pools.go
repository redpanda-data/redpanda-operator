// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package render

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
	redpandav1alpha3 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha3"
)

func renderPool(dot *helmette.Dot, pool *redpandav1alpha3.NodePool, pools []*redpandav1alpha3.NodePool) *appsv1.StatefulSet {
	values := helmette.Unwrap[Values](dot.Values)

	ss := &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "StatefulSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", Fullname(dot), pool.Name),
			Namespace: dot.Release.Namespace,
			Labels:    FullLabels(dot),
		},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: NodePoolLabelsSelector(dot, pool),
			},
			ServiceName:         ServiceName(dot),
			Replicas:            pool.Spec.Replicas,
			UpdateStrategy:      appsv1.StatefulSetUpdateStrategy{Type: appsv1.OnDeleteStatefulSetStrategyType},
			PodManagementPolicy: "Parallel",
			Template: StrategicMergePatch(PodTemplate{
				Labels: helmette.Merge(NodePoolLabelsSelector(dot, pool), map[string]string{
					"cluster.redpanda.com/broker":      "true",
					"redpanda.com/poddisruptionbudget": Fullname(dot),
				}),
				Annotations: map[string]string{
					"config.redpanda.com/checksum": statefulSetChecksumAnnotation(dot, pools),
				},
				Spec: pool.Spec.BrokerTemplate.PodTemplate.Spec,
			}, corev1.PodTemplateSpec{
				Spec: defaultPoolSpec(dot, pool),
			}),
			VolumeClaimTemplates: nil, // Set below
		},
	}

	// VolumeClaimTemplates
	if values.Storage.PersistentVolume.Enabled || (values.Storage.IsTieredStorageEnabled() && values.Storage.TieredMountType() == "persistentVolume") {
		if t := volumeClaimTemplateDatadir(dot); t != nil {
			ss.Spec.VolumeClaimTemplates = append(ss.Spec.VolumeClaimTemplates, *t)
		}
		if t := volumeClaimTemplateTieredStorageDir(dot); t != nil {
			ss.Spec.VolumeClaimTemplates = append(ss.Spec.VolumeClaimTemplates, *t)
		}
	}

	return ss
}

func poolDefaultInitContainers(dot *helmette.Dot, pool *redpandav1alpha3.NodePool) []corev1.Container {
	values := helmette.Unwrap[Values](dot.Values)

	var containers []corev1.Container

	redpandaImage := helmette.Default(fmt.Sprintf("%s:%s", values.Image.Repository, Tag(dot)), pool.Spec.BrokerTemplate.Images.Redpanda)
	initImage := helmette.Default("busybox:latest", pool.Spec.BrokerTemplate.Images.InitContainer)
	sidecarImage := helmette.Default("redpanda/redpanda-operator:latest", pool.Spec.BrokerTemplate.Images.Sidecar)

	if values.Tuning.TuneAIOEvents {
		tuner := helmette.Default("all", helmette.Join(" ", pool.Spec.BrokerTemplate.Tuning))
		containers = append(containers, corev1.Container{
			Name:    "tuning",
			Image:   redpandaImage,
			Command: []string{`/bin/bash`, `-c`, fmt.Sprintf(`rpk redpanda tune %s`, tuner)},
			SecurityContext: &corev1.SecurityContext{
				Capabilities: &corev1.Capabilities{
					Add: []corev1.Capability{`SYS_RESOURCE`},
				},
				Privileged: ptr.To(true),
				RunAsUser:  ptr.To(int64(0)),
				RunAsGroup: ptr.To(int64(0)),
			},
			VolumeMounts: append(CommonMounts(dot),
				corev1.VolumeMount{
					Name:      "base-config",
					MountPath: "/etc/redpanda",
				},
			),
		})
	}

	if pool.Spec.BrokerTemplate.SetDataDirectoryOwnership {
		containers = append(containers, corev1.Container{
			Name:  "set-datadir-ownership",
			Image: initImage,
			Command: []string{
				`/bin/sh`,
				`-c`,
				`chown 101:101 -R /var/lib/redpanda/data`,
			},
			VolumeMounts: append(
				CommonMounts(dot),
				corev1.VolumeMount{
					Name:      `datadir`,
					MountPath: `/var/lib/redpanda/data`,
				},
			),
			SecurityContext: &corev1.SecurityContext{
				RunAsUser:  ptr.To[int64](0),
				RunAsGroup: ptr.To[int64](0),
				// needed to change ownership of PVs that default to root:root
				Privileged: ptr.To(true),
			},
		})
	}

	if pool.Spec.BrokerTemplate.ValidateFilesystem {
		containers = append(containers, corev1.Container{
			Name:    "fs-validator",
			Image:   initImage,
			Command: []string{`/bin/sh`},
			Args: []string{
				`-c`,
				`trap "exit 0" TERM; exec /etc/secrets/fs-validator/scripts/fsValidator.sh xfs & wait $!`,
			},
			VolumeMounts: append(
				CommonMounts(dot),
				corev1.VolumeMount{
					Name:      fmt.Sprintf(`%.49s-fs-validator`, Fullname(dot)),
					MountPath: `/etc/secrets/fs-validator/scripts/`,
				},
				corev1.VolumeMount{
					Name:      `datadir`,
					MountPath: `/var/lib/redpanda/data`,
				},
			),
		})
	}

	if values.Storage.IsTieredStorageEnabled() {
		cacheDir := values.Storage.TieredCacheDirectory(dot)
		mounts := append(CommonMounts(dot), corev1.VolumeMount{
			Name:      "datadir",
			MountPath: "/var/lib/redpanda/data",
		})
		if values.Storage.TieredMountType() != "none" {
			name := "tiered-storage-dir"
			if values.Storage.PersistentVolume != nil && values.Storage.PersistentVolume.NameOverwrite != "" {
				name = values.Storage.PersistentVolume.NameOverwrite
			}
			mounts = append(mounts, corev1.VolumeMount{
				Name:      name,
				MountPath: cacheDir,
			})
		}

		containers = append(containers, corev1.Container{
			Name:  `set-tiered-storage-cache-dir-ownership`,
			Image: initImage,
			Command: []string{
				`/bin/sh`,
				`-c`,
				fmt.Sprintf(`mkdir -p %s; chown 101:101 -R %s`, cacheDir, cacheDir),
			},
			SecurityContext: &corev1.SecurityContext{
				RunAsUser:  ptr.To[int64](0),
				RunAsGroup: ptr.To[int64](0),
				// needed to change ownership of PVs that default to root:root
				Privileged: ptr.To(true),
			},
			VolumeMounts: mounts,
		})
	}

	{
		configuratorMounts := append(
			CommonMounts(dot),
			corev1.VolumeMount{
				Name:      "config",
				MountPath: "/etc/redpanda",
			},
			corev1.VolumeMount{
				Name:      "base-config",
				MountPath: "/tmp/base-config",
			},
			corev1.VolumeMount{
				Name:      fmt.Sprintf(`%.51s-configurator`, Fullname(dot)),
				MountPath: "/etc/secrets/configurator/scripts/",
			},
		)
		if values.RackAwareness.Enabled {
			configuratorMounts = append(configuratorMounts, corev1.VolumeMount{
				Name:      ServiceAccountVolumeName,
				MountPath: DefaultAPITokenMountPath,
				ReadOnly:  true,
			})
		}

		containers = append(containers, corev1.Container{
			Name:  fmt.Sprintf(`%.51s-configurator`, Name(dot)),
			Image: redpandaImage,
			Command: []string{
				`/bin/bash`, `-c`,
				`trap "exit 0" TERM; exec $CONFIGURATOR_SCRIPT "${SERVICE_NAME}" "${KUBERNETES_NODE_NAME}" & wait $!`,
			},
			Env: rpkEnvVars(dot, []corev1.EnvVar{
				{
					Name:  "CONFIGURATOR_SCRIPT",
					Value: "/etc/secrets/configurator/scripts/configurator.sh",
				},
				{
					Name: "SERVICE_NAME",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: "metadata.name",
						},
						ResourceFieldRef: nil,
						ConfigMapKeyRef:  nil,
						SecretKeyRef:     nil,
					},
				},
				{
					Name: "KUBERNETES_NODE_NAME",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: "spec.nodeName",
						},
					},
				},
				{
					Name: "HOST_IP_ADDRESS",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							APIVersion: "v1",
							FieldPath:  "status.hostIP",
						},
					},
				},
			}),
			VolumeMounts: configuratorMounts,
		})
	}

	{
		env := values.Storage.Tiered.CredentialsSecretRef.AsEnvVars(values.Storage.GetTieredStorageConfig())
		_, _, additionalEnv := values.Config.ExtraClusterConfiguration.Translate()

		containers = append(containers, corev1.Container{
			Name:  "bootstrap-yaml-envsubst",
			Image: sidecarImage,
			Command: []string{
				"/redpanda-operator",
				"bootstrap",
				"--in-dir",
				"/tmp/base-config",
				"--out-dir",
				"/tmp/config",
			},
			Env: append(env, additionalEnv...),
			Resources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("100m"),
					corev1.ResourceMemory: resource.MustParse("125Mi"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("100m"),
					corev1.ResourceMemory: resource.MustParse("125Mi"),
				},
			},
			SecurityContext: &corev1.SecurityContext{
				// NB: RunAsUser and RunAsGroup will be inherited from the
				// PodSecurityContext of consumers.
				AllowPrivilegeEscalation: ptr.To(false),
				ReadOnlyRootFilesystem:   ptr.To(true),
				RunAsNonRoot:             ptr.To(true),
			},
			VolumeMounts: []corev1.VolumeMount{
				{Name: "config", MountPath: "/tmp/config/"},
				{Name: "base-config", MountPath: "/tmp/base-config/"},
			},
		})
	}

	return containers
}

func poolDefaultVolumes(dot *helmette.Dot, pool *redpandav1alpha3.NodePool) []corev1.Volume {
	values := helmette.Unwrap[Values](dot.Values)

	fullname := Fullname(dot)
	volumes := CommonVolumes(dot)

	// NOTE and tiered-storage-dir are NOT in this
	// function. TODO: Migrate them into this function.
	volumes = append(volumes, []corev1.Volume{
		{
			Name: "lifecycle-scripts",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  fmt.Sprintf("%.50s-sts-lifecycle", fullname),
					DefaultMode: ptr.To[int32](0o775),
				},
			},
		},
		{
			Name: "base-config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: fullname},
				},
			},
		},
		{
			Name: "config",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
		{
			Name: fmt.Sprintf("%.51s-configurator", fullname),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  fmt.Sprintf("%.51s-configurator", fullname),
					DefaultMode: ptr.To[int32](0o775),
				},
			},
		},
	}...)

	if pool.Spec.BrokerTemplate.ValidateFilesystem {
		volumes = append(volumes, corev1.Volume{
			Name: fmt.Sprintf("%.49s-fs-validator", fullname),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  fmt.Sprintf("%.49s-fs-validator", fullname),
					DefaultMode: ptr.To[int32](0o775),
				},
			},
		})
	}

	if vol := values.Listeners.TrustStoreVolume(&values.TLS); vol != nil {
		volumes = append(volumes, *vol)
	}

	volumes = append(volumes, statefulSetVolumeDataDir(dot))

	if v := statefulSetVolumeTieredStorageDir(dot); v != nil {
		volumes = append(volumes, *v)
	}

	volumes = append(volumes, kubeTokenAPIVolume(ServiceAccountVolumeName))

	return nil
}

func poolDefaultContainers(dot *helmette.Dot, pool *redpandav1alpha3.NodePool) []corev1.Container {
	values := helmette.Unwrap[Values](dot.Values)
	internalAdvertiseAddress := fmt.Sprintf("%s.%s", "$(SERVICE_NAME)", InternalDomain(dot))
	var containers []corev1.Container

	redpandaImage := helmette.Default(fmt.Sprintf("%s:%s", values.Image.Repository, Tag(dot)), pool.Spec.BrokerTemplate.Images.Redpanda)
	sidecarImage := helmette.Default("redpanda/redpanda-operator:latest", pool.Spec.BrokerTemplate.Images.Sidecar)

	{
		container := corev1.Container{
			Name:  Name(dot),
			Image: redpandaImage,
			Env:   bootstrapEnvVars(dot, statefulSetRedpandaEnv()),
			Lifecycle: &corev1.Lifecycle{
				// finish the lifecycle scripts with "true" to prevent them from terminating the pod prematurely
				PostStart: &corev1.LifecycleHandler{
					Exec: &corev1.ExecAction{
						Command: wrapLifecycleHook(
							"post-start",
							90/2, // half of default terminationGracePeriodSeconds
							[]string{"bash", "-x", "/var/lifecycle/postStart.sh"},
						),
					},
				},
				PreStop: &corev1.LifecycleHandler{
					Exec: &corev1.ExecAction{
						Command: wrapLifecycleHook(
							"pre-stop",
							90/2, // half of default terminationGracePeriodSeconds
							[]string{"bash", "-x", "/var/lifecycle/preStop.sh"},
						),
					},
				},
			},
			StartupProbe: &corev1.Probe{
				// the startupProbe checks to see that the admin api is listening and that the broker has a node_id assigned. This
				// check is only used to delay the start of the liveness and readiness probes until it passes.
				ProbeHandler: corev1.ProbeHandler{
					Exec: &corev1.ExecAction{
						Command: []string{
							`/bin/sh`,
							`-c`,
							helmette.Join("\n", []string{
								`set -e`,
								fmt.Sprintf(`RESULT=$(curl --silent --fail -k -m 5 %s "%s://%s/v1/status/ready")`,
									adminTLSCurlFlags(dot),
									adminInternalHTTPProtocol(dot),
									adminApiURLs(dot),
								),
								`echo $RESULT`,
								`echo $RESULT | grep ready`,
								``,
							}),
						},
					},
				},
				InitialDelaySeconds: 1,
				PeriodSeconds:       10,
				FailureThreshold:    120,
			},
			LivenessProbe: &corev1.Probe{
				// the livenessProbe just checks to see that the admin api is listening and returning 200s.
				ProbeHandler: corev1.ProbeHandler{
					Exec: &corev1.ExecAction{
						Command: []string{
							`/bin/sh`,
							`-c`,
							fmt.Sprintf(`curl --silent --fail -k -m 5 %s "%s://%s/v1/status/ready"`,
								adminTLSCurlFlags(dot),
								adminInternalHTTPProtocol(dot),
								adminApiURLs(dot),
							),
						},
					},
				},
				InitialDelaySeconds: 10,
				PeriodSeconds:       10,
				FailureThreshold:    3,
			},
			Command: []string{
				`rpk`, `redpanda`, `start`,
				fmt.Sprintf(`--advertise-rpc-addr=%s:%d`,
					internalAdvertiseAddress,
					values.Listeners.RPC.Port,
				),
			},
			VolumeMounts: StatefulSetVolumeMounts(dot),
			SecurityContext: &corev1.SecurityContext{
				RunAsUser:  ptr.To[int64](101),
				RunAsGroup: ptr.To[int64](101),
				Privileged: ptr.To(false),
			},
		}

		// admin http kafka schemaRegistry rpc
		container.Ports = append(container.Ports, corev1.ContainerPort{
			Name:          "admin",
			ContainerPort: values.Listeners.Admin.Port,
		})
		for externalName, external := range helmette.SortedMap(values.Listeners.Admin.External) {
			if external.IsEnabled() {
				// The original template used
				// $external.port > 0 &&
				// [ $external.enabled ||
				//   (values.External.Enabled && (dig "enabled" true $external)
				// ]
				// ... which is equivalent to the above check
				container.Ports = append(container.Ports, corev1.ContainerPort{
					Name:          fmt.Sprintf("admin-%.8s", helmette.Lower(externalName)),
					ContainerPort: external.Port,
				})
			}
		}
		container.Ports = append(container.Ports, corev1.ContainerPort{
			Name:          "http",
			ContainerPort: values.Listeners.HTTP.Port,
		})
		for externalName, external := range helmette.SortedMap(values.Listeners.HTTP.External) {
			if external.IsEnabled() {
				container.Ports = append(container.Ports, corev1.ContainerPort{
					Name:          fmt.Sprintf("http-%.8s", helmette.Lower(externalName)),
					ContainerPort: external.Port,
				})
			}
		}
		container.Ports = append(container.Ports, corev1.ContainerPort{
			Name:          "kafka",
			ContainerPort: values.Listeners.Kafka.Port,
		})
		for externalName, external := range helmette.SortedMap(values.Listeners.Kafka.External) {
			if external.IsEnabled() {
				container.Ports = append(container.Ports, corev1.ContainerPort{
					Name:          fmt.Sprintf("kafka-%.8s", helmette.Lower(externalName)),
					ContainerPort: external.Port,
				})
			}
		}
		container.Ports = append(container.Ports, corev1.ContainerPort{
			Name:          "rpc",
			ContainerPort: values.Listeners.RPC.Port,
		})
		container.Ports = append(container.Ports, corev1.ContainerPort{
			Name:          "schemaregistry",
			ContainerPort: values.Listeners.SchemaRegistry.Port,
		})
		for externalName, external := range helmette.SortedMap(values.Listeners.SchemaRegistry.External) {
			if external.IsEnabled() {
				container.Ports = append(container.Ports, corev1.ContainerPort{
					Name:          fmt.Sprintf("schema-%.8s", helmette.Lower(externalName)),
					ContainerPort: external.Port,
				})
			}
		}

		if values.Storage.IsTieredStorageEnabled() && values.Storage.TieredMountType() != "none" {
			name := "tiered-storage-dir"
			if values.Storage.PersistentVolume != nil && values.Storage.PersistentVolume.NameOverwrite != "" {
				name = values.Storage.PersistentVolume.NameOverwrite
			}
			container.VolumeMounts = append(container.VolumeMounts,
				corev1.VolumeMount{
					Name:      name,
					MountPath: values.Storage.TieredCacheDirectory(dot),
				},
			)
		}

		containers = append(containers, container)
	}

	{
		args := []string{
			`/redpanda-operator`,
			`sidecar`,
			`--redpanda-yaml`,
			`/etc/redpanda/redpanda.yaml`,
			`--redpanda-cluster-namespace`,
			dot.Release.Namespace,
			`--redpanda-cluster-name`,
			Fullname(dot),
			`--run-broker-probe`,
			`--broker-probe-broker-url`,
			// even though this is named "...URLs", it returns
			// only the url for the given pod
			adminURLsCLI(dot),
		}

		volumeMounts := append(
			CommonMounts(dot),
			corev1.VolumeMount{
				Name:      "config",
				MountPath: "/etc/redpanda",
			},
			corev1.VolumeMount{
				Name:      ServiceAccountVolumeName,
				MountPath: DefaultAPITokenMountPath,
				ReadOnly:  true,
			},
		)

		containers = append(containers, corev1.Container{
			Name:    "sidecar",
			Image:   sidecarImage,
			Command: []string{`/redpanda-operator`},
			Args:    append([]string{`supervisor`, `--`}, args...),
			Env:     append(rpkEnvVars(dot, nil), statefulSetRedpandaEnv()...),
			SecurityContext: &corev1.SecurityContext{
				RunAsUser:  ptr.To[int64](101),
				RunAsGroup: ptr.To[int64](101),
				Privileged: ptr.To(false),
			},
			VolumeMounts: volumeMounts,
			ReadinessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{
						// This endpoint is the endpoint for the modified health probe initialized by
						// the sidecar. When it's hit it performs an authenticated request to the cluster
						// and ensures that the broker is in a "healthy" state, i.e. it doesn't have
						// under-replicated partitions, it's part of the cluster quorum, etc.
						Path: "/healthz",
						Port: intstr.FromInt32(8093),
					},
				},
				InitialDelaySeconds: 1,
				PeriodSeconds:       10,
				SuccessThreshold:    1,
				FailureThreshold:    3,
			},
		})
	}
	return containers
}

func defaultPoolSpec(dot *helmette.Dot, pool *redpandav1alpha3.NodePool) corev1.PodSpec {
	values := helmette.Unwrap[Values](dot.Values)

	return corev1.PodSpec{
		AutomountServiceAccountToken:  ptr.To(false),
		TerminationGracePeriodSeconds: ptr.To(int64(90)),
		ImagePullSecrets:              helmette.Default(nil, values.ImagePullSecrets),
		ServiceAccountName:            ServiceAccountName(dot),
		InitContainers:                poolDefaultInitContainers(dot, pool),
		Containers:                    poolDefaultContainers(dot, pool),
		Volumes:                       poolDefaultVolumes(dot, pool),
		TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
			{
				MaxSkew:           1,
				TopologyKey:       "topology.kubernetes.io/zone",
				WhenUnsatisfiable: corev1.ScheduleAnyway,
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: ClusterPodLabelsSelector(dot),
				},
			},
		},
		NodeSelector: nil,
		Affinity: &corev1.Affinity{
			PodAffinity: nil,
			PodAntiAffinity: &corev1.PodAntiAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
					{
						TopologyKey: "kubernetes.io/hostname",
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: ClusterPodLabelsSelector(dot),
						},
					},
				},
			},
		},
		SecurityContext: &corev1.PodSecurityContext{
			FSGroup:             ptr.To[int64](101),
			RunAsUser:           ptr.To[int64](101),
			FSGroupChangePolicy: ptr.To(corev1.FSGroupChangeOnRootMismatch),
		},
		PriorityClassName: "",
	}
}
