// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package conversion

import (
	"fmt"

	"github.com/cockroachdb/errors"
	applycorev1 "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
)

// V2Defaults contains the default values for the v2 CRD conversion.
type V2Defaulters struct {
	RedpandaImage    func(*redpandav1alpha2.RedpandaImage) *redpandav1alpha2.RedpandaImage
	SidecarImage     func(*redpandav1alpha2.RedpandaImage) *redpandav1alpha2.RedpandaImage
	ConfiguratorArgs []string
}

// ConvertV2ToRenderState converts a v2 Redpanda CRD to a redpanda chart RenderState.
func ConvertV2ToRenderState(config *kube.RESTConfig, defaulters *V2Defaulters, cluster *redpandav1alpha2.Redpanda, pools []*redpandav1alpha2.NodePool) (*redpanda.RenderState, error) {
	spec := defaultV2Spec(defaulters, cluster)

	dot, err := redpanda.Chart.Dot(config, helmette.Release{
		Namespace: cluster.Namespace,
		Name:      cluster.GetHelmReleaseName(),
		Service:   "Helm",
		IsUpgrade: true,
	}, spec)
	if err != nil {
		return nil, err
	}

	return redpanda.RenderStateFromDot(dot, func(state *redpanda.RenderState) error {
		if err := convertV2Fields(state, &state.Values, spec); err != nil {
			return err
		}
		pools, err := convertV2NodepoolsToPools(pools, defaulters)
		if err != nil {
			return err
		}
		state.Pools = pools
		return nil
	})
}

// defaultV2Spec defaults the v2 Redpanda CRD spec to avoid nil dereferences during chart construction.
func defaultV2Spec(defaulters *V2Defaulters, cluster *redpandav1alpha2.Redpanda) *redpandav1alpha2.RedpandaClusterSpec {
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

	// Use the default image passed on the command-line.
	if defaulters.RedpandaImage != nil {
		spec.Image = defaulters.RedpandaImage(spec.Image)
	}

	// The flag that disables cluster configuration synchronization is set to `true` to not
	// conflict with operator cluster configuration synchronization.
	spec.Statefulset.SideCars.Args = []string{"--no-set-superusers"}

	// If not explicitly specified, set the initContainer flags for the bootstrap
	// templating to instantiate an appropriate CloudExpander
	if defaulters.SidecarImage != nil {
		spec.Statefulset.SideCars.Image = defaulters.SidecarImage(spec.Statefulset.SideCars.Image)
		spec.Statefulset.SideCars.Controllers.Image = defaulters.SidecarImage(spec.Statefulset.SideCars.Controllers.Image)
	}

	return spec
}

// convertV2Fields converts the v2 Redpanda CRD fields to the redpanda chart values based on the changes introduced in
// https://github.com/redpanda-data/redpanda-operator/pull/602.
//
// This function is responsible for the following field mappings:
// - `nodeSelector` -> `podTemplate.spec.nodeSelector`
// - `affinity` -> `podTemplate.spec.affinity`
// - `tolerations` -> `podTemplate.spec.tolerations`
// - `imagePullSecrets` -> `podTemplate.spec.imagePullSecrets`.
//
// All other field mappings happen in nested conversion functions.
func convertV2Fields(state *redpanda.RenderState, values *redpanda.Values, spec *redpandav1alpha2.RedpandaClusterSpec) error {
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

	return convertStatefulsetV2Fields(state, values, spec.Statefulset)
}

// The `convertStatefulsetV2Fields` function is responsible for the following field mappings:
// - `statefulset.annotations` -> `statefulset.podTemplate.annotations`
// - `statefulset.nodeSelector` -> `statefulset.podTemplate.spec.nodeSelector`
// - `statefulset.priorityClassName` -> `statefulset.podTemplate.spec.priorityClassName`
// - `statefulset.terminationGracePeriodSeconds` -> `statefulset.podTemplate.spec.terminationGracePeriodSeconds`
// - `statefulset.livenessProbe` -> `statefulset.podTemplate.spec.containers[0].livenessProbe`
// - `statefulset.startupProbe` -> `statefulset.podTemplate.spec.containers[0].startupProbe`
// - `statefulset.readinessProbe` -> `statefulset.podTemplate.spec.containers[1].readinessProbe`
// - `statefulset.tolerations` -> `statefulset.podTemplate.spec.tolerations`
// - `statefulset.topologySpreadConstraints` -> `statefulset.podTemplate.spec.topologySpreadConstraints`
// - `statefulset.podAffinity` -> `statefulset.podTemplate.spec.affinity.podAffinity`
// - `statefulset.podAntiAffinity` -> `statefulset.podTemplate.spec.affinity.podAntiAffinity`
// - `statefulset.extraVolumes` -> `statefulset.podTemplate.spec.volumes`
// - `statefulset.extraVolumesMounts` -> `statefulset.podTemplate.spec.containers[*].volumeMounts`
//
// All other field mappings for init and sidecar containers happen in nested conversion functions.
func convertStatefulsetV2Fields(state *redpanda.RenderState, values *redpanda.Values, spec *redpandav1alpha2.Statefulset) error {
	if spec == nil {
		return nil
	}

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

	if spec.Annotations != nil {
		values.Statefulset.PodTemplate.Annotations = spec.Annotations
	}
	if spec.NodeSelector != nil {
		values.Statefulset.PodTemplate.Spec.NodeSelector = spec.NodeSelector
	}
	if spec.PriorityClassName != nil {
		values.Statefulset.PodTemplate.Spec.PriorityClassName = spec.PriorityClassName
	}
	if spec.TerminationGracePeriodSeconds != nil {
		values.Statefulset.PodTemplate.Spec.TerminationGracePeriodSeconds = ptr.To(int64(*spec.TerminationGracePeriodSeconds))
	}

	if err := convertJSONNotNil(spec.LivenessProbe, redpandaContainer.LivenessProbe); err != nil {
		return err
	}
	if err := convertJSONNotNil(spec.StartupProbe, redpandaContainer.StartupProbe); err != nil {
		return err
	}
	if err := convertJSONNotNil(spec.ReadinessProbe, sidecarContainer.ReadinessProbe); err != nil {
		return err
	}
	if err := convertAndAppendJSONNotNil(spec.Tolerations, &values.Statefulset.PodTemplate.Spec.Tolerations); err != nil {
		return err
	}
	if err := convertAndAppendJSONNotNil(spec.TopologySpreadConstraints, &values.Statefulset.PodTemplate.Spec.TopologySpreadConstraints); err != nil {
		return err
	}
	if values.Statefulset.PodTemplate.Spec.Affinity == nil {
		values.Statefulset.PodTemplate.Spec.Affinity = &applycorev1.AffinityApplyConfiguration{}
	}
	if err := convertAndInitializeAffinityNotNil(spec.PodAffinity, values.Statefulset.PodTemplate.Spec.Affinity); err != nil {
		return err
	}
	convertAndInitializeAntiAffinityNotNil(state, spec.PodAntiAffinity, values.Statefulset.PodTemplate.Spec.Affinity)
	if err := convertAndAppendYAMLNotNil(state, spec.ExtraVolumes, &values.Statefulset.PodTemplate.Spec.Volumes); err != nil {
		return err
	}
	if err := convertAndAppendYAMLNotNil(state, spec.ExtraVolumeMounts, &redpandaContainer.VolumeMounts); err != nil {
		return err
	}
	if err := convertAndAppendYAMLNotNil(state, spec.ExtraVolumeMounts, &sidecarContainer.VolumeMounts); err != nil {
		return err
	}

	if err := convertStatefulsetInitContainersV2Fields(state, values, spec.InitContainers); err != nil {
		return err
	}

	return convertStatefulsetSidecarV2Fields(state, values, spec.SideCars)
}

// The `convertStatefulsetInitContainersV2Fields` function is responsible for the following field mappings:
// - `statefulset.initContainers.extraInitContainers` -> `statefulset.podTemplate.spec.initContainers`
// - `statefulset.initContainers.*.extraVolumesMounts` -> `statefulset.podTemplate.spec.initContainers[*].volumeMounts`
// - `statefulset.initContainers.*.resources` -> `statefulset.podTemplate.spec.initContainers[*].resources`
func convertStatefulsetInitContainersV2Fields(state *redpanda.RenderState, values *redpanda.Values, spec *redpandav1alpha2.InitContainers) error {
	if spec == nil {
		return nil
	}

	if err := convertAndAppendYAMLNotNil(state, spec.ExtraInitContainers, &values.Statefulset.PodTemplate.Spec.InitContainers); err != nil {
		return err
	}

	if err := convertInitContainer(state, values, redpanda.RedpandaConfiguratorContainerName, spec.Configurator); err != nil {
		return err
	}

	// NB: we need to check if the following containers are enabled first, otherwise we wind up with a badly merged pod template spec.
	if values.Tuning.TuneAIOEvents {
		if err := convertInitContainer(state, values, redpanda.RedpandaTuningContainerName, spec.Tuning); err != nil {
			return err
		}
	}
	if values.Statefulset.InitContainers.SetDataDirOwnership.Enabled {
		if err := convertInitContainer(state, values, redpanda.SetDataDirectoryOwnershipContainerName, spec.SetDataDirOwnership); err != nil {
			return err
		}
	}
	if values.Storage.IsTieredStorageEnabled() {
		if err := convertInitContainer(state, values, redpanda.SetTieredStorageCacheOwnershipContainerName, spec.SetTieredStorageCacheDirOwnership); err != nil {
			return err
		}
	}
	if values.Statefulset.InitContainers.FSValidator.Enabled {
		if err := convertInitContainer(state, values, redpanda.FSValidatorContainerName, spec.FsValidator); err != nil {
			return err
		}
	}

	return nil
}

// The `convertStatefulsetSidecarV2Fields` function is responsible for the following field mappings:
// - `statefulset.sidecars.extraVolumeMounts` -> `statefulset.podTemplate.spec.containers[1].volumeMounts`
// - `statefulset.sidecars.resources` -> `statefulset.podTemplate.spec.containers[1].resources`
// - `statefulset.sidecars.securityContext` -> `statefulset.podTemplate.spec.containers[1].securityContext`
func convertStatefulsetSidecarV2Fields(state *redpanda.RenderState, values *redpanda.Values, spec *redpandav1alpha2.SideCars) error {
	if spec == nil {
		return nil
	}

	sidecarContainer := containerOrInit(&values.Statefulset.PodTemplate.Spec.Containers, redpanda.SidecarContainerName)

	if err := convertAndAppendYAMLNotNil(state, spec.ExtraVolumeMounts, &sidecarContainer.VolumeMounts); err != nil {
		return err
	}
	if err := convertJSONNotNil(spec.Resources, sidecarContainer.Resources); err != nil {
		return err
	}

	if err := convertJSONNotNil(spec.SecurityContext, sidecarContainer.SecurityContext); err != nil {
		return err
	}

	return nil
}

func convertV2NodepoolsToPools(pools []*redpandav1alpha2.NodePool, defaulters *V2Defaulters) ([]redpanda.Pool, error) {
	converted := make([]redpanda.Pool, len(pools))
	for i, pool := range pools {
		set, err := convertV2NodepoolToPool(pool, defaulters)
		if err != nil {
			return nil, err
		}
		converted[i] = set
	}
	return converted, nil
}

func convertV2NodepoolToPool(pool *redpandav1alpha2.NodePool, defaulters *V2Defaulters) (_ redpanda.Pool, err error) {
	// we grab *just* the default values here
	v, err := redpanda.Chart.LoadValues(map[string]any{})
	if err != nil {
		return redpanda.Pool{}, err
	}
	defer func() {
		switch r := recover().(type) {
		case nil:
		case error:
			err = errors.Wrapf(r, "yaml conversion failed")
		default:
			err = errors.Newf("yaml conversion failed: %#v", r)
		}
	}()

	values := helmette.Unwrap[redpanda.Values](v)
	defaultSet := values.Statefulset
	// we adjust some of the defaults that need to be changed in the nodepool context
	defaultSet.PodTemplate.Spec.Affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution[0].LabelSelector.MatchLabels = map[string]string{
		`app.kubernetes.io/component`: fmt.Sprintf(`{{ include "redpanda.name" . }}-%s-statefulset`, pool.Name),
		`app.kubernetes.io/instance`:  `{{ .Release.Name }}`,
		`app.kubernetes.io/name`:      `{{ include "redpanda.name" . }}`,
	}
	defaultSet.PodTemplate.Spec.TopologySpreadConstraints[0].LabelSelector.MatchLabels = map[string]string{
		`app.kubernetes.io/component`: fmt.Sprintf(`{{ include "redpanda.name" . }}-%s-statefulset`, pool.Name),
		`app.kubernetes.io/instance`:  `{{ .Release.Name }}`,
		`app.kubernetes.io/name`:      `{{ include "redpanda.name" . }}`,
	}

	// next we default our images
	if defaulters.RedpandaImage != nil {
		pool.Spec.Image = defaulters.RedpandaImage(pool.Spec.Image)
	}
	if defaulters.SidecarImage != nil {
		pool.Spec.SidecarImage = defaulters.SidecarImage(pool.Spec.SidecarImage)
	}

	// now we merge everything in to construct the pool
	if err := convertJSON(pool.Spec, &defaultSet); err != nil {
		return redpanda.Pool{}, err
	}

	// and do a little bit of conversion
	if err := convertJSON(pool.Spec.SidecarImage, &defaultSet.SideCars.Image); err != nil {
		return redpanda.Pool{}, err
	}

	// and finally return wrapped with a name
	return redpanda.Pool{
		Name:        pool.Name,
		Statefulset: defaultSet,
	}, nil
}
