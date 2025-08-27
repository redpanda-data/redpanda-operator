// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package lifecycle

import (
	"context"
	"encoding/json"

	appsv1 "k8s.io/api/apps/v1"
	applycorev1 "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	redpandav1alpha3 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha3"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
)

// V2NodePoolRenderer represents a node pool renderer for v2 clusters.
type V2NodePoolRenderer struct {
	kubeConfig    *kube.RESTConfig
	sideCarImage  Image
	redpandaImage Image
	cloudSecrets  CloudSecretsFlags
}

var _ NodePoolRenderer[ClusterWithPools, *ClusterWithPools] = (*V2NodePoolRenderer)(nil)

// NewV2NodePoolRenderer returns a V2NodePoolRenderer.
func NewV2NodePoolRenderer(mgr ctrl.Manager, redpandaImage, sideCarImage Image, cloudSecrets CloudSecretsFlags) *V2NodePoolRenderer {
	return &V2NodePoolRenderer{
		kubeConfig:    mgr.GetConfig(),
		sideCarImage:  sideCarImage,
		redpandaImage: redpandaImage,
		cloudSecrets:  cloudSecrets,
	}
}

// Render returns a list of StatefulSets for the given Redpanda v2 cluster. It does this by
// delegating to our particular resource rendering pipeline and filtering out anything that
// isn't a node pool.
func (m *V2NodePoolRenderer) Render(ctx context.Context, cluster *ClusterWithPools) ([]*appsv1.StatefulSet, error) {
	// Big ol' block of defaulting to avoid nil dereferences.
	spec := cluster.Spec.ClusterSpec.DeepCopy()
	if spec == nil {
		spec = &redpandav1alpha2.RedpandaClusterSpec{}
	}

	if spec.Statefulset == nil {
		spec.Statefulset = &redpandav1alpha2.Statefulset{}
	}

	if spec.Statefulset.SideCars == nil {
		spec.Statefulset.SideCars = &redpandav1alpha2.SideCars{}
	}

	if spec.Statefulset.SideCars.Controllers == nil {
		spec.Statefulset.SideCars.Controllers = &redpandav1alpha2.RPControllers{}
	}

	if spec.Statefulset.InitContainers == nil {
		spec.Statefulset.InitContainers = &redpandav1alpha2.InitContainers{}
	}

	if spec.Statefulset.InitContainers.Configurator == nil {
		spec.Statefulset.InitContainers.Configurator = &redpandav1alpha2.Configurator{}
	}

	// As we're currently pinned to the v5.10.x chart, we need to set the
	// default image to the operator's preferred version.
	spec.Image = defaultImage(spec.Image, m.redpandaImage)

	// The flag that disables cluster configuration synchronization is set to `true` to not
	// conflict with operator cluster configuration synchronization.
	spec.Statefulset.SideCars.Args = []string{"--no-set-superusers"}

	// If not explicitly specified, set the tag and repository of the sidecar
	// to the image specified via CLI args rather than relying on the default
	// of the redpanda chart.
	// This ensures that custom deployments (e.g.
	// localhost/redpanda-operator:dev) will use the image they are deployed
	// with.
	spec.Statefulset.SideCars.Image = defaultImage(spec.Statefulset.SideCars.Image, m.sideCarImage)
	spec.Statefulset.SideCars.Controllers.Image = defaultImage(spec.Statefulset.SideCars.Image, m.sideCarImage)

	// If not explicitly specified, set the initContainer flags for the bootstrap
	// templating to instantiate an appropriate CloudExpander
	if len(spec.Statefulset.InitContainers.Configurator.AdditionalCLIArgs) == 0 {
		spec.Statefulset.InitContainers.Configurator.AdditionalCLIArgs = m.cloudSecrets.AdditionalConfiguratorArgs()
	}

	state, err := constructRenderState(m.kubeConfig, cluster.Namespace, cluster.GetHelmReleaseName(), spec, cluster.NodePools)
	if err != nil {
		return nil, err
	}

	return redpanda.RenderNodePools(state)
}

func isNodePool(object client.Object) bool {
	_, ok := object.(*appsv1.StatefulSet)
	return ok
}

// IsNodePool returns whether or not the object passed to it should be considered a node pool.
func (m *V2NodePoolRenderer) IsNodePool(object client.Object) bool {
	return isNodePool(object)
}

func defaultImage(base *redpandav1alpha2.RedpandaImage, default_ Image) *redpandav1alpha2.RedpandaImage {
	if base == nil {
		return &redpandav1alpha2.RedpandaImage{
			Repository: ptr.To(default_.Repository),
			Tag:        ptr.To(default_.Tag),
		}
	}

	return &redpandav1alpha2.RedpandaImage{
		Repository: ptr.To(ptr.Deref(base.Repository, default_.Repository)),
		Tag:        ptr.To(ptr.Deref(base.Tag, default_.Tag)),
	}
}

func constructRenderState(config *kube.RESTConfig, namespace, release string, spec *redpandav1alpha2.RedpandaClusterSpec, _pools []*redpandav1alpha3.NodePool) (*redpanda.RenderState, error) {
	dot, err := redpanda.Chart.Dot(config, helmette.Release{
		Namespace: namespace,
		Name:      release,
		Service:   "Helm",
		IsUpgrade: true,
	}, spec)
	if err != nil {
		return nil, err
	}

	return redpanda.RenderStateFromDot(dot, func(values redpanda.Values) error {
		if values.PodTemplate.Spec == nil {
			values.PodTemplate.Spec = &applycorev1.PodSpecApplyConfiguration{}
		}
		if spec.NodeSelector != nil {
			values.PodTemplate.Spec.NodeSelector = spec.NodeSelector
		}
		if err := convertJSONNotNil(spec.Affinity, values.PodTemplate.Spec.Affinity); err != nil {
			return err
		}
		if err := convertAndAppendJSONNotNil(spec.Tolerations, &values.PodTemplate.Spec.Tolerations); err != nil {
			return err
		}

		if err := convertAndAppendJSONNotNil(spec.ImagePullSecrets, &values.PodTemplate.Spec.ImagePullSecrets); err != nil {
			return err
		}

		if spec.Statefulset != nil {
			if values.Statefulset.PodTemplate.Spec == nil {
				values.Statefulset.PodTemplate.Spec = &applycorev1.PodSpecApplyConfiguration{}
			}
			if values.Statefulset.PodTemplate.Spec.Containers == nil {
				values.Statefulset.PodTemplate.Spec.Containers = []applycorev1.ContainerApplyConfiguration{}
			}
			if values.Statefulset.PodTemplate.Spec.InitContainers == nil {
				values.Statefulset.PodTemplate.Spec.InitContainers = []applycorev1.ContainerApplyConfiguration{}
			}

			redpandaContainer := containerOrInit(&values.Statefulset.PodTemplate.Spec.Containers, redpanda.RedpandaContainerName)
			sidecarContainer := containerOrInit(&values.Statefulset.PodTemplate.Spec.Containers, redpanda.SidecarContainerName)
			configuratorContainer := containerOrInit(&values.Statefulset.PodTemplate.Spec.InitContainers, redpanda.RedpandaConfiguratorContainerName)
			tuningContainer := containerOrInit(&values.Statefulset.PodTemplate.Spec.InitContainers, redpanda.RedpandaTuningContainerName)
			setDataDirectoryContainer := containerOrInit(&values.Statefulset.PodTemplate.Spec.InitContainers, redpanda.SetDataDirectoryOwnershipContainerName)
			setTieredStorageDirectoryContainer := containerOrInit(&values.Statefulset.PodTemplate.Spec.InitContainers, redpanda.SetTieredStorageCacheOwnershipContainerName)
			fsValidatorContainer := containerOrInit(&values.Statefulset.PodTemplate.Spec.InitContainers, redpanda.FSValidatorContainerName)

			if spec.Statefulset.Annotations != nil {
				values.Statefulset.PodTemplate.Annotations = spec.Statefulset.Annotations
			}
			if err := convertJSONNotNil(spec.Statefulset.LivenessProbe, redpandaContainer.LivenessProbe); err != nil {
				return err
			}
			if err := convertJSONNotNil(spec.Statefulset.StartupProbe, redpandaContainer.StartupProbe); err != nil {
				return err
			}
			if err := convertJSONNotNil(spec.Statefulset.ReadinessProbe, sidecarContainer.ReadinessProbe); err != nil {
				return err
			}
			if err := convertAndInitializeJSONNotNil(spec.Statefulset.PodAffinity, values.Statefulset.PodTemplate.Spec.Affinity); err != nil {
				return err
			}
			if spec.Statefulset.NodeSelector != nil {
				values.Statefulset.PodTemplate.Spec.NodeSelector = spec.Statefulset.NodeSelector
			}
			if spec.Statefulset.PriorityClassName != nil {
				values.Statefulset.PodTemplate.Spec.PriorityClassName = spec.Statefulset.PriorityClassName
			}

			if err := convertAndAppendJSONNotNil(spec.Statefulset.Tolerations, &values.Statefulset.PodTemplate.Spec.Tolerations); err != nil {
				return err
			}
			if err := convertAndAppendJSONNotNil(spec.Statefulset.TopologySpreadConstraints, &values.Statefulset.PodTemplate.Spec.TopologySpreadConstraints); err != nil {
				return err
			}
			if spec.Statefulset.TerminationGracePeriodSeconds != nil {
				values.Statefulset.PodTemplate.Spec.TerminationGracePeriodSeconds = ptr.To[int64](int64(*spec.Statefulset.TerminationGracePeriodSeconds))
			}

			// TODO: handle these fields as they seem to have been converted from a string to a raw extension array
			// if spec.Statefulset.ExtraVolumes != nil {
			// 	if values.Statefulset.PodTemplate.Spec.Volumes == nil {
			// 		values.Statefulset.PodTemplate.Spec.Volumes = []applycorev1.VolumeApplyConfiguration{}
			// 	}
			// 	if err := convertJSON(spec.Statefulset.ExtraVolumes, &values.Statefulset.PodTemplate.Spec.Volumes); err != nil {
			// 		return err
			// 	}
			// }

			if spec.Statefulset.InitContainers != nil {
				// TODO: handle these later as they go from string to array
				// if spec.Statefulset.InitContainers.ExtraInitContainers != nil {
				// 	extraContainers := []applycorev1.ContainerApplyConfiguration{}
				// 	if err := convertJSON(spec.Statefulset.InitContainers.ExtraInitContainers, &extraContainers); err != nil {
				// 		return err
				// 	}
				// 	values.Statefulset.PodTemplate.Spec.InitContainers = append(values.Statefulset.PodTemplate.Spec.InitContainers, extraContainers...)
				// }

				if spec.Statefulset.InitContainers.Configurator != nil {
					if err := convertJSONNotNil(spec.Statefulset.InitContainers.Configurator.Resources, configuratorContainer.Resources); err != nil {
						return err
					}
					// TODO: volume mounts as they go from a string to an array
				}
				if spec.Statefulset.InitContainers.Tuning != nil {
					if err := convertJSONNotNil(spec.Statefulset.InitContainers.Tuning.Resources, tuningContainer.Resources); err != nil {
						return err
					}
					// TODO: volume mounts as they go from a string to an array
				}
				if spec.Statefulset.InitContainers.SetDataDirOwnership != nil {
					if err := convertJSONNotNil(spec.Statefulset.InitContainers.SetDataDirOwnership.Resources, setDataDirectoryContainer.Resources); err != nil {
						return err
					}
					// TODO: volume mounts as they go from a string to an array
				}
				if spec.Statefulset.InitContainers.SetTieredStorageCacheDirOwnership != nil {
					if err := convertJSONNotNil(spec.Statefulset.InitContainers.SetTieredStorageCacheDirOwnership.Resources, setTieredStorageDirectoryContainer.Resources); err != nil {
						return err
					}
					// TODO: volume mounts as they go from a string to an array
				}
				if spec.Statefulset.InitContainers.FsValidator != nil {
					if err := convertJSONNotNil(spec.Statefulset.InitContainers.FsValidator.Resources, fsValidatorContainer.Resources); err != nil {
						return err
					}
					// TODO: volume mounts as they go from a string to an array
				}

				// TODO: extra init container conversion as it goes from a string to array
				// statefulset.initContainers.extraInitContainers -> statefulset.podTemplate.spec.initContainers
			}

			if spec.Statefulset.SideCars != nil {
				if spec.Statefulset.SideCars.ConfigWatcher != nil {
					// TODO: extra volume mounts as they go from a string to an array
					// statefulset.sidecars.configWatcher.extraVolumeMounts -> statefulset.podTemplate.spec.containers[*].volumeMounts
					if err := convertJSONNotNil(spec.Statefulset.SideCars.ConfigWatcher.Resources, sidecarContainer.Resources); err != nil {
						return err
					}

					if err := convertJSONNotNil(spec.Statefulset.SideCars.ConfigWatcher.SecurityContext, sidecarContainer.SecurityContext); err != nil {
						return err
					}

					// TODO: should we handle sidecars.controllers.resources/securityContext? the controllers and sidecar are the same container
					// TODO: handle extra volume mounts as they go from a string to an array
					// statefulset.sidecars.configWatcher.extraVolumeMounts -> statefulset.podTemplate.spec.containers[*].volumeMounts
				}
			}
		}
		return nil
	})
}

func convertJSON(from, to any) error {
	data, err := json.Marshal(from)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, to)
}

func convertAndInitializeJSONNotNil[T any, U *T, V any, W *V](from U, to W) error {
	if from == nil {
		return nil
	}

	if to == nil {
		var v V
		to = &v
	}

	return convertJSON(from, to)
}

func convertAndAppendJSONNotNil[T any, V any](from []T, to *[]V) error {
	if from == nil {
		return nil
	}

	converted := []V{}
	if err := convertJSON(from, &converted); err != nil {
		return err
	}
	*to = append(*to, converted...)
	return nil
}

func convertJSONNotNil[T any, U *T](from U, to any) error {
	if from == nil {
		return nil
	}
	return convertJSON(from, to)
}

func containerOrInit(containers *[]applycorev1.ContainerApplyConfiguration, name string) *applycorev1.ContainerApplyConfiguration {
	for i := range *containers {
		container := &(*containers)[i]
		if ptr.Deref(container.Name, "") == name {
			return container
		}
	}
	container := applycorev1.ContainerApplyConfiguration{
		Name: ptr.To(name),
	}
	*containers = append(*containers, container)
	return &(*containers)[len(*containers)-1]
}
