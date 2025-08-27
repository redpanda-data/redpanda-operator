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

func constructRenderState(config *kube.RESTConfig, namespace, release string, spec *redpandav1alpha2.RedpandaClusterSpec, pools []*redpandav1alpha3.NodePool) (*redpanda.RenderState, error) {
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
		if spec.Affinity != nil {
			if values.PodTemplate.Spec.Affinity == nil {
				values.PodTemplate.Spec.Affinity = &applycorev1.AffinityApplyConfiguration{}
			}
			if err := convertJSON(spec.Affinity, values.PodTemplate.Spec.Affinity); err != nil {
				return err
			}
		}
		if spec.Tolerations != nil {
			if values.PodTemplate.Spec.Tolerations == nil {
				values.PodTemplate.Spec.Tolerations = []applycorev1.TolerationApplyConfiguration{}
			}
			if err := convertJSON(spec.Tolerations, &values.PodTemplate.Spec.Tolerations); err != nil {
				return err
			}
		}
		if spec.ImagePullSecrets != nil {
			if values.PodTemplate.Spec.ImagePullSecrets == nil {
				values.PodTemplate.Spec.ImagePullSecrets = []applycorev1.LocalObjectReferenceApplyConfiguration{}
			}
			if err := convertJSON(spec.ImagePullSecrets, &values.PodTemplate.Spec.ImagePullSecrets); err != nil {
				return err
			}
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

			redpandaContainer := firstOrInit(&values.Statefulset.PodTemplate.Spec.Containers, func(c applycorev1.ContainerApplyConfiguration) bool {
				return ptr.Deref(c.Name, "") == redpanda.RedpandaContainerName
			}, applycorev1.ContainerApplyConfiguration{
				Name: ptr.To(redpanda.RedpandaContainerName),
			})
			sidecarContainer := firstOrInit(&values.Statefulset.PodTemplate.Spec.Containers, func(c applycorev1.ContainerApplyConfiguration) bool {
				return ptr.Deref(c.Name, "") == redpanda.SidecarContainerName
			}, applycorev1.ContainerApplyConfiguration{
				Name: ptr.To(redpanda.SidecarContainerName),
			})
			configuratorContainer := firstOrInit(&values.Statefulset.PodTemplate.Spec.InitContainers, func(c applycorev1.ContainerApplyConfiguration) bool {
				return ptr.Deref(c.Name, "") == redpanda.RedpandaConfiguratorContainerName
			}, applycorev1.ContainerApplyConfiguration{
				Name: ptr.To(redpanda.RedpandaConfiguratorContainerName),
			})
			tuningContainer := firstOrInit(&values.Statefulset.PodTemplate.Spec.InitContainers, func(c applycorev1.ContainerApplyConfiguration) bool {
				return ptr.Deref(c.Name, "") == redpanda.RedpandaTuningContainerName
			}, applycorev1.ContainerApplyConfiguration{
				Name: ptr.To(redpanda.RedpandaTuningContainerName),
			})
			setDataDirectoryContainer := firstOrInit(&values.Statefulset.PodTemplate.Spec.InitContainers, func(c applycorev1.ContainerApplyConfiguration) bool {
				return ptr.Deref(c.Name, "") == redpanda.SetDataDirectoryOwnershipContainerName
			}, applycorev1.ContainerApplyConfiguration{
				Name: ptr.To(redpanda.SetDataDirectoryOwnershipContainerName),
			})
			setTieredStorageDirectoryContainer := firstOrInit(&values.Statefulset.PodTemplate.Spec.InitContainers, func(c applycorev1.ContainerApplyConfiguration) bool {
				return ptr.Deref(c.Name, "") == redpanda.SetTieredStorageCacheOwnershipContainerName
			}, applycorev1.ContainerApplyConfiguration{
				Name: ptr.To(redpanda.SetTieredStorageCacheOwnershipContainerName),
			})
			fsValidatorContainer := firstOrInit(&values.Statefulset.PodTemplate.Spec.InitContainers, func(c applycorev1.ContainerApplyConfiguration) bool {
				return ptr.Deref(c.Name, "") == redpanda.FSValidatorContainerName
			}, applycorev1.ContainerApplyConfiguration{
				Name: ptr.To(redpanda.FSValidatorContainerName),
			})

			if spec.Statefulset.Annotations != nil {
				values.Statefulset.PodTemplate.Annotations = spec.Statefulset.Annotations
			}
			if spec.Statefulset.LivenessProbe != nil {
				if err := convertJSON(spec.Statefulset.LivenessProbe, redpandaContainer.LivenessProbe); err != nil {
					return err
				}
			}
			if spec.Statefulset.StartupProbe != nil {
				if err := convertJSON(spec.Statefulset.StartupProbe, redpandaContainer.StartupProbe); err != nil {
					return err
				}
			}
			if spec.Statefulset.ReadinessProbe != nil {
				if err := convertJSON(spec.Statefulset.ReadinessProbe, sidecarContainer.ReadinessProbe); err != nil {
					return err
				}
			}
			if spec.Statefulset.PodAffinity != nil {
				if values.Statefulset.PodTemplate.Spec.Affinity == nil {
					values.Statefulset.PodTemplate.Spec.Affinity = &applycorev1.AffinityApplyConfiguration{}
				}
				if err := convertJSON(spec.Statefulset.PodAffinity, values.Statefulset.PodTemplate.Spec.Affinity); err != nil {
					return err
				}
			}
			if spec.Statefulset.NodeSelector != nil {
				values.Statefulset.PodTemplate.Spec.NodeSelector = spec.Statefulset.NodeSelector
			}
			if spec.Statefulset.PriorityClassName != nil {
				values.Statefulset.PodTemplate.Spec.PriorityClassName = spec.Statefulset.PriorityClassName
			}
			if spec.Statefulset.Tolerations != nil {
				tolerations := []applycorev1.TolerationApplyConfiguration{}
				if err := convertJSON(spec.Statefulset.Tolerations, &tolerations); err != nil {
					return err
				}
				values.Statefulset.PodTemplate.Spec.Tolerations = append(values.Statefulset.PodTemplate.Spec.Tolerations, tolerations...)
			}
			if spec.Statefulset.TopologySpreadConstraints != nil {
				topologyContraints := []applycorev1.TopologySpreadConstraintApplyConfiguration{}
				if err := convertJSON(spec.Statefulset.TopologySpreadConstraints, &topologyContraints); err != nil {
					return err
				}
				values.Statefulset.PodTemplate.Spec.TopologySpreadConstraints = append(values.Statefulset.PodTemplate.Spec.TopologySpreadConstraints, topologyContraints...)
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
				if spec.Statefulset.InitContainers.ExtraInitContainers != nil {
					extraContainers := []applycorev1.ContainerApplyConfiguration{}
					if err := convertJSON(spec.Statefulset.InitContainers.ExtraInitContainers, &extraContainers); err != nil {
						return err
					}
					values.Statefulset.PodTemplate.Spec.InitContainers = append(values.Statefulset.PodTemplate.Spec.InitContainers, extraContainers...)
				}

				if spec.Statefulset.InitContainers.Configurator != nil {
					if spec.Statefulset.InitContainers.Configurator.Resources != nil {
						if err := convertJSON(spec.Statefulset.InitContainers.Configurator.Resources, configuratorContainer.Resources); err != nil {
							return err
						}
					}
					// TODO: volume mounts as they go from a string to an array
				}
				if spec.Statefulset.InitContainers.Tuning != nil {
					if spec.Statefulset.InitContainers.Tuning.Resources != nil {
						if err := convertJSON(spec.Statefulset.InitContainers.Tuning.Resources, tuningContainer.Resources); err != nil {
							return err
						}
					}
					// TODO: volume mounts as they go from a string to an array
				}
				if spec.Statefulset.InitContainers.SetDataDirOwnership != nil {
					if spec.Statefulset.InitContainers.SetDataDirOwnership.Resources != nil {
						if err := convertJSON(spec.Statefulset.InitContainers.SetDataDirOwnership.Resources, setDataDirectoryContainer.Resources); err != nil {
							return err
						}
					}
					// TODO: volume mounts as they go from a string to an array
				}
				if spec.Statefulset.InitContainers.SetTieredStorageCacheDirOwnership != nil {
					if spec.Statefulset.InitContainers.SetTieredStorageCacheDirOwnership.Resources != nil {
						if err := convertJSON(spec.Statefulset.InitContainers.SetTieredStorageCacheDirOwnership.Resources, setTieredStorageDirectoryContainer.Resources); err != nil {
							return err
						}
					}
					// TODO: volume mounts as they go from a string to an array
				}
				if spec.Statefulset.InitContainers.FsValidator != nil {
					if spec.Statefulset.InitContainers.FsValidator.Resources != nil {
						if err := convertJSON(spec.Statefulset.InitContainers.FsValidator.Resources, fsValidatorContainer.Resources); err != nil {
							return err
						}
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
					if spec.Statefulset.SideCars.ConfigWatcher.Resources != nil {
						if err := convertJSON(spec.Statefulset.SideCars.ConfigWatcher.Resources, sidecarContainer.Resources); err != nil {
							return err
						}
					}

					if spec.Statefulset.SideCars.ConfigWatcher.SecurityContext != nil {
						if sidecarContainer.SecurityContext == nil {
							sidecarContainer.SecurityContext = &applycorev1.SecurityContextApplyConfiguration{}
						}
						if err := convertJSON(spec.Statefulset.SideCars.ConfigWatcher.SecurityContext, sidecarContainer.SecurityContext); err != nil {
							return err
						}
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

func firstOrInit[T any](slice *[]T, cond func(T) bool, v T) *T {
	for _, entry := range *slice {
		if cond(entry) {
			return &entry
		}
	}
	*slice = append(*slice, v)
	return &v
}
