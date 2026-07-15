// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package pipeline

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"

	"github.com/cockroachdb/errors"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/redpanda-data/common-go/kube"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/yaml"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

const (
	// Default resource requests for Connect pods.
	defaultMemoryRequest = "256Mi"
	defaultCPURequest    = "100m"

	zoneTopologyKey = "topology.kubernetes.io/zone"
)

// render implements [kube.Renderer] for Pipeline resources.
type render struct {
	pipeline          *redpandav1alpha2.Pipeline
	labels            map[string]string
	commonAnnotations map[string]string
	// defaultImage is the chart-level default Redpanda Connect image, used
	// when the Pipeline CR omits .spec.image. Empty when the operator was
	// installed without setting connectController.image.{repository,tag}.
	// Falls through to the PipelineDefaultImage constant when empty.
	defaultImage string
	monitoring   MonitoringConfig
	// clusterConn holds resolved Redpanda cluster connection details when
	// the Pipeline references a cluster via clusterRef. Nil when no
	// clusterRef is configured.
	clusterConn *clusterConnection
	// userCredentials holds the SASL identity the pipeline authenticates
	// as when bound to a cluster via .userRef. Nil when no userRef is set
	// (which is also the case for staticConfiguration and inline-only
	// pipelines).
	userCredentials *userCredentials
	// licenseContent is the raw bytes of the operator-level enterprise
	// license. When non-empty, the renderer mirrors it into a Pipeline-owned
	// Secret and injects REDPANDA_LICENSE into the Connect container so
	// enterprise inputs (mysql_cdc, etc.) pass their own runtime license
	// gate. Empty when no license is configured.
	licenseContent []byte
}

// licenseSecretSuffix is appended to the Pipeline name to derive the
// per-Pipeline Secret that mirrors the operator's license.
const licenseSecretSuffix = "-license"

// licenseSecretKey is the key inside the per-Pipeline license Secret that
// holds the license bytes.
const licenseSecretKey = "license"

// Types returns the set of Kubernetes resource types managed by the Pipeline
// controller.
func Types() []kube.Object {
	return []kube.Object{
		&appsv1.Deployment{},
		&corev1.ConfigMap{},
		&corev1.Secret{},
		&policyv1.PodDisruptionBudget{},
		&monitoringv1.PodMonitor{},
	}
}

func (r *render) Types() []kube.Object {
	return Types()
}

// Render produces all Kubernetes objects for the given Pipeline.
func (r *render) Render(_ context.Context) ([]kube.Object, error) {
	cm, err := r.configMap()
	if err != nil {
		return nil, err
	}

	dp := r.deployment()

	// Stamp the pod template with a checksum of the rendered config so a
	// configYaml/configFiles change rolls the Deployment. Connect only reads
	// its config at startup and the config is mounted from a ConfigMap, so
	// without this a config change would not restart the pods.
	if dp.Spec.Template.Annotations == nil {
		dp.Spec.Template.Annotations = map[string]string{}
	}
	dp.Spec.Template.Annotations["cluster.redpanda.com/config-checksum"] = configChecksum(cm.Data)

	objs := []kube.Object{cm, dp}

	if sec := r.licenseSecret(); sec != nil {
		objs = append(objs, sec)
	}

	if pdb := r.podDisruptionBudget(); pdb != nil {
		objs = append(objs, pdb)
	}

	if pm := r.podMonitor(); pm != nil {
		objs = append(objs, pm)
	}

	return objs, nil
}

// licenseSecret returns a Pipeline-owned Secret holding the operator's
// license bytes, or nil when no license content is available.
func (r *render) licenseSecret() *corev1.Secret {
	if len(r.licenseContent) == 0 {
		return nil
	}
	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        r.pipeline.Name + licenseSecretSuffix,
			Namespace:   r.pipeline.Namespace,
			Labels:      r.labels,
			Annotations: r.annotations(),
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			licenseSecretKey: r.licenseContent,
		},
	}
}

// Labels returns the standard set of labels applied to Pipeline-owned
// resources.
func Labels(pipeline *redpandav1alpha2.Pipeline) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       "redpanda-connect",
		"app.kubernetes.io/instance":   pipeline.Name,
		"app.kubernetes.io/managed-by": "redpanda-operator",
		"app.kubernetes.io/component":  "connect-pipeline",
	}
}

func (r *render) annotations() map[string]string {
	if len(r.commonAnnotations) == 0 {
		return nil
	}
	out := make(map[string]string, len(r.commonAnnotations))
	for k, v := range r.commonAnnotations {
		out[k] = v
	}
	return out
}

// podAnnotations returns annotations for the pod template, merging
// commonAnnotations with per-pipeline spec.annotations. Per-pipeline
// annotations take precedence.
func (r *render) podAnnotations() map[string]string {
	specAnn := r.pipeline.Spec.Annotations
	if len(r.commonAnnotations) == 0 && len(specAnn) == 0 {
		return nil
	}
	out := make(map[string]string, len(r.commonAnnotations)+len(specAnn))
	for k, v := range r.commonAnnotations {
		out[k] = v
	}
	for k, v := range specAnn {
		out[k] = v
	}
	return out
}

func (r *render) configMap() (*corev1.ConfigMap, error) {
	rendered, err := r.renderConnectYAML()
	if err != nil {
		return nil, err
	}

	data := map[string]string{
		"connect.yaml": rendered,
	}
	for filename, content := range r.pipeline.Spec.ConfigFiles {
		if filename == "connect.yaml" {
			return nil, errors.New("configFiles cannot contain a key named \"connect.yaml\"; use configYaml instead")
		}
		data[filename] = content
	}

	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        r.pipeline.Name,
			Namespace:   r.pipeline.Namespace,
			Labels:      r.labels,
			Annotations: r.annotations(),
		},
		Data: data,
	}, nil
}

// renderConnectYAML returns the connect.yaml the pipeline pod will see. When
// the Pipeline is bound to a Redpanda cluster via .cluster.clusterRef or
// .cluster.staticConfiguration, the operator inline-merges the resolved
// connection fields (seed_brokers, tls, sasl) into:
//   - any `input.redpanda` / `output.redpanda` blocks the user wrote, and
//   - a top-level `redpanda:` block, which is the shared client the
//     `redpanda_common` input/output plugins (and any other shared-client
//     consumer) bind to.
//
// User-side keys win on conflict: if the user already set seed_brokers, tls,
// or sasl on a redpanda block (nested or top-level), their values are left
// as-is. That's the escape hatch for pipelines that need a different cluster,
// an override TLS setting, etc.
func (r *render) renderConnectYAML() (string, error) {
	configYAML := r.pipeline.Spec.ConfigYAML

	overlay := r.redpandaPluginOverlay()
	if len(overlay) == 0 {
		return configYAML, nil
	}

	var userConfig map[string]any
	if err := yaml.Unmarshal([]byte(configYAML), &userConfig); err != nil {
		return "", errors.Wrap(err, "configYaml is not valid YAML")
	}
	if userConfig == nil {
		userConfig = map[string]any{}
	}

	// Inline-merge into input.redpanda and output.redpanda blocks. Missing
	// blocks are not synthesized — if the user's config has no
	// output.redpanda (e.g. they're writing to S3 only) we don't inject
	// one.
	mergeRedpandaPlugin(userConfig, "input", overlay)
	mergeRedpandaPlugin(userConfig, "output", overlay)

	// Always expose the resolved connection as the top-level `redpanda:`
	// shared client, so `redpanda_common` (and any plugin that relies on the
	// shared client) is usable without inline credentials. User-set top-level
	// keys win.
	mergeTopLevelRedpanda(userConfig, overlay)

	out, err := yaml.Marshal(userConfig)
	if err != nil {
		return "", errors.Wrap(err, "marshalling rendered connect.yaml")
	}
	return string(out), nil
}

// redpandaPluginOverlay returns the connection fields the operator will
// merge into `input.redpanda` / `output.redpanda` blocks. Empty when the
// pipeline has no cluster binding (the fully-inline configYaml case). Both
// cluster sources (clusterRef and staticConfiguration) arrive here as a
// resolved clusterConnection.
func (r *render) redpandaPluginOverlay() map[string]any {
	if r.clusterConn == nil {
		return nil
	}

	overlay := map[string]any{}

	if len(r.clusterConn.Brokers) > 0 {
		seeds := make([]any, 0, len(r.clusterConn.Brokers))
		for _, b := range r.clusterConn.Brokers {
			seeds = append(seeds, b)
		}
		overlay["seed_brokers"] = seeds
	}

	if tls := r.clusterConn.TLS; tls != nil {
		block := map[string]any{"enabled": true}
		// The CA is projected into the pod under its source key (see
		// buildClusterConnectionResources); the rendered path must use that
		// same key. A TLS cluster without a custom CA (publicly-issued certs)
		// gets enabled: true with no root_cas_file.
		if key := tls.caCertKey(); key != "" {
			block["root_cas_file"] = clusterTLSMountPath + "/" + key
		}
		if tls.InsecureSkipVerify {
			block["skip_cert_verify"] = true
		}
		overlay["tls"] = block
	}

	// The SASL identity: a userRef always wins; otherwise fall back to the
	// credentials resolved from the cluster source (the bootstrap user for
	// clusterRef, the inline sasl block for staticConfiguration). Both are
	// projected as REDPANDA_SASL_* env vars, which this block references.
	switch {
	case r.userCredentials != nil:
		overlay["sasl"] = []any{map[string]any{
			"mechanism": r.userCredentials.Mechanism,
			"username":  "${REDPANDA_SASL_USERNAME}",
			"password":  "${REDPANDA_SASL_PASSWORD}",
		}}
	case r.clusterConn.SASL != nil:
		overlay["sasl"] = []any{map[string]any{
			"mechanism": r.clusterConn.SASL.Mechanism,
			"username":  "${REDPANDA_SASL_USERNAME}",
			"password":  "${REDPANDA_SASL_PASSWORD}",
		}}
	}

	return overlay
}

// mergeRedpandaPlugin overlays connection fields onto the `redpanda` block
// nested under root[section] (section is "input" or "output"). User-side
// keys are preserved; only keys the user did NOT set get filled in from
// overlay. No-op if root[section].redpanda is missing or not a map.
func mergeRedpandaPlugin(root map[string]any, section string, overlay map[string]any) {
	if len(overlay) == 0 {
		return
	}
	sectionMap, ok := root[section].(map[string]any)
	if !ok {
		return
	}
	plugin, ok := sectionMap["redpanda"].(map[string]any)
	if !ok {
		return
	}
	for k, v := range overlay {
		if _, exists := plugin[k]; exists {
			continue
		}
		plugin[k] = v
	}
	sectionMap["redpanda"] = plugin
	root[section] = sectionMap
}

// configChecksum returns a deterministic sha256 over the ConfigMap data so the
// pod template changes (and the Deployment rolls) whenever the rendered
// config changes. Keys are sorted for stability across reconciles.
func configChecksum(data map[string]string) string {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		// sha256's Write never returns an error.
		_, _ = fmt.Fprintf(h, "%s\x00%s\x00", k, data[k])
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// mergeTopLevelRedpanda overlays connection fields onto the top-level
// `redpanda:` shared-client block (used by redpanda_common and other
// shared-client plugins). It creates the block if absent; user-set keys are
// preserved.
func mergeTopLevelRedpanda(root map[string]any, overlay map[string]any) {
	if len(overlay) == 0 {
		return
	}
	plugin, ok := root["redpanda"].(map[string]any)
	if !ok {
		plugin = map[string]any{}
	}
	for k, v := range overlay {
		if _, exists := plugin[k]; exists {
			continue
		}
		plugin[k] = v
	}
	root["redpanda"] = plugin
}

const (
	// clusterTLSVolumeName is the volume name for the CA certificate from the referenced cluster.
	clusterTLSVolumeName = "cluster-tls-ca"
	// clusterTLSMountPath is the mount path for the cluster CA certificate.
	clusterTLSMountPath = "/etc/tls/certs/ca"
)

// resolveImage picks the Redpanda Connect image for the Pipeline pod using
// the three-tier precedence:
//
//  1. .spec.image on the Pipeline CR (per-pipeline override; always wins).
//  2. .defaultImage on the renderer (chart-level default, set via the
//     operator's --connect-default-image flag — itself derived from
//     connectController.image.{repository,tag} in the operator chart).
//  3. redpandav1alpha2.PipelineDefaultImage (binary-baked fallback).
func (r *render) resolveImage() string {
	if r.pipeline.Spec.Image != nil && *r.pipeline.Spec.Image != "" {
		return *r.pipeline.Spec.Image
	}
	if r.defaultImage != "" {
		return r.defaultImage
	}
	return redpandav1alpha2.PipelineDefaultImage
}

func (r *render) deployment() *appsv1.Deployment {
	replicas := r.pipeline.GetReplicas()
	image := r.resolveImage()

	resources := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse(defaultMemoryRequest),
			corev1.ResourceCPU:    resource.MustParse(defaultCPURequest),
		},
	}
	if r.pipeline.Spec.Resources != nil {
		resources = *r.pipeline.Spec.Resources
	}

	env := buildValueSourceEnv(r.pipeline)
	if len(r.licenseContent) > 0 {
		env = append(env, corev1.EnvVar{
			Name: "REDPANDA_LICENSE",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: r.pipeline.Name + licenseSecretSuffix,
					},
					Key: licenseSecretKey,
				},
			},
		})
	}
	// SASL credentials for the redpanda input/output are projected when the
	// pipeline is bound to a cluster via .userRef. The Pipeline YAML can
	// reference these as ${REDPANDA_SASL_USERNAME} / ${REDPANDA_SASL_PASSWORD}
	// / ${REDPANDA_SASL_MECHANISM}, or rely on the operator-generated
	// `redpanda` block in connect.yaml (which references them internally).
	switch {
	case r.userCredentials != nil:
		env = append(env, r.userCredentials.envVars()...)
	case r.clusterConn != nil && r.clusterConn.SASL != nil:
		// No userRef: project the cluster-source credentials (bootstrap user
		// for clusterRef, inline sasl for staticConfiguration) under the same
		// REDPANDA_SASL_* names the generated `sasl:` block references.
		env = append(env, r.clusterConn.SASL.envVars()...)
	}
	volumes := []corev1.Volume{
		{
			Name: "config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: r.pipeline.Name,
					},
				},
			},
		},
	}
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "config",
			MountPath: "/config",
			ReadOnly:  true,
		},
	}

	// Inject cluster connection env vars and TLS volumes when clusterRef is set.
	if cc := r.clusterConn; cc != nil {
		clusterEnv, clusterVolumes, clusterMounts := buildClusterConnectionResources(cc)
		env = append(env, clusterEnv...)
		volumes = append(volumes, clusterVolumes...)
		volumeMounts = append(volumeMounts, clusterMounts...)
	}

	// User-supplied init containers run to completion, in order, ahead of the
	// built-in lint container — so anything they stage into a shared volume is
	// visible to lint and to the connect runtime.
	initContainers := make([]corev1.Container, 0, len(r.pipeline.Spec.ExtraInitContainers)+1)
	initContainers = append(initContainers, r.pipeline.Spec.ExtraInitContainers...)
	initContainers = append(initContainers, corev1.Container{
		Name:                     "lint",
		Image:                    image,
		Command:                  []string{"/redpanda-connect", "lint", "/config/connect.yaml"},
		Env:                      env,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		VolumeMounts:             volumeMounts,
	})

	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        r.pipeline.Name,
			Namespace:   r.pipeline.Namespace,
			Labels:      r.labels,
			Annotations: r.annotations(),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(replicas),
			Selector: &metav1.LabelSelector{
				MatchLabels: r.labels,
			},
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RecreateDeploymentStrategyType,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      r.labels,
					Annotations: r.podAnnotations(),
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: r.pipeline.Spec.ServiceAccountName,
					InitContainers:     initContainers,
					Containers: []corev1.Container{
						{
							Name:    "connect",
							Image:   image,
							Command: []string{"/redpanda-connect", "run", "/config/connect.yaml"},
							Ports: []corev1.ContainerPort{
								{Name: "http", ContainerPort: 4195, Protocol: corev1.ProtocolTCP},
							},
							Env:       env,
							Resources: resources,
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/ready",
										Port: intstr.FromInt32(4195),
									},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       10,
							},
							VolumeMounts: volumeMounts,
						},
					},
					Volumes:                   volumes,
					NodeSelector:              r.pipeline.Spec.NodeSelector,
					Tolerations:               r.pipeline.Spec.Tolerations,
					Affinity:                  buildAffinity(r.pipeline),
					TopologySpreadConstraints: buildTopologySpreadConstraints(r.pipeline, r.labels),
				},
			},
		},
	}
}

func (r *render) podDisruptionBudget() *policyv1.PodDisruptionBudget {
	budget := r.pipeline.Spec.Budget
	if budget == nil {
		return nil
	}

	maxUnavailable := intstr.FromInt32(int32(budget.MaxUnavailable))

	return &policyv1.PodDisruptionBudget{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "policy/v1",
			Kind:       "PodDisruptionBudget",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        r.pipeline.Name,
			Namespace:   r.pipeline.Namespace,
			Labels:      r.labels,
			Annotations: r.annotations(),
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MaxUnavailable: &maxUnavailable,
			Selector: &metav1.LabelSelector{
				MatchLabels: r.labels,
			},
		},
	}
}

// buildClusterConnectionResources returns the env vars, volumes, and volume
// mounts needed to connect to a Redpanda cluster via clusterRef.
func buildClusterConnectionResources(cc *clusterConnection) ([]corev1.EnvVar, []corev1.Volume, []corev1.VolumeMount) {
	var env []corev1.EnvVar
	var volumes []corev1.Volume
	var mounts []corev1.VolumeMount

	env = append(env, corev1.EnvVar{
		Name:  "RPK_BROKERS",
		Value: cc.BrokersString(),
	})

	if cc.TLS != nil {
		env = append(env, corev1.EnvVar{
			Name:  "RPK_TLS_ENABLED",
			Value: "true",
		})

		// Project the custom CA (if any) under its source key. A TLS cluster
		// without a custom CA (publicly-issued certs) mounts nothing.
		var projection *corev1.VolumeProjection
		switch {
		case cc.TLS.CACertSecretRef != nil:
			projection = &corev1.VolumeProjection{
				Secret: &corev1.SecretProjection{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: cc.TLS.CACertSecretRef.Name,
					},
					Items: []corev1.KeyToPath{
						{
							Key:  cc.TLS.CACertSecretRef.Key,
							Path: cc.TLS.CACertSecretRef.Key,
						},
					},
				},
			}
		case cc.TLS.CACertConfigMapRef != nil:
			projection = &corev1.VolumeProjection{
				ConfigMap: &corev1.ConfigMapProjection{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: cc.TLS.CACertConfigMapRef.Name,
					},
					Items: []corev1.KeyToPath{
						{
							Key:  cc.TLS.CACertConfigMapRef.Key,
							Path: cc.TLS.CACertConfigMapRef.Key,
						},
					},
				},
			}
		}

		if projection != nil {
			env = append(env, corev1.EnvVar{
				Name:  "RPK_TLS_ROOT_CAS_FILE",
				Value: fmt.Sprintf("%s/%s", clusterTLSMountPath, cc.TLS.caCertKey()),
			})

			volumes = append(volumes, corev1.Volume{
				Name: clusterTLSVolumeName,
				VolumeSource: corev1.VolumeSource{
					Projected: &corev1.ProjectedVolumeSource{
						Sources: []corev1.VolumeProjection{*projection},
					},
				},
			})

			mounts = append(mounts, corev1.VolumeMount{
				Name:      clusterTLSVolumeName,
				MountPath: clusterTLSMountPath,
				ReadOnly:  true,
			})
		}
	} else {
		env = append(env, corev1.EnvVar{
			Name:  "RPK_TLS_ENABLED",
			Value: "false",
		})
	}

	if cc.SASL != nil {
		env = append(env, corev1.EnvVar{
			Name:  "RPK_SASL_MECHANISM",
			Value: cc.SASL.Mechanism,
		}, corev1.EnvVar{
			Name:  "RPK_SASL_USER",
			Value: cc.SASL.Username,
		})

		switch {
		case cc.SASL.PasswordRef != nil:
			env = append(env, corev1.EnvVar{
				Name: "RPK_SASL_PASSWORD",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: cc.SASL.PasswordRef,
				},
			})
		case cc.SASL.PasswordConfigMapRef != nil:
			env = append(env, corev1.EnvVar{
				Name: "RPK_SASL_PASSWORD",
				ValueFrom: &corev1.EnvVarSource{
					ConfigMapKeyRef: cc.SASL.PasswordConfigMapRef,
				},
			})
		case cc.SASL.PasswordValue != "":
			env = append(env, corev1.EnvVar{
				Name:  "RPK_SASL_PASSWORD",
				Value: cc.SASL.PasswordValue,
			})
		}
	}

	return env, volumes, mounts
}

func (r *render) podMonitor() *monitoringv1.PodMonitor {
	if !r.monitoring.Enabled {
		return nil
	}

	labels := make(map[string]string, len(r.labels)+len(r.monitoring.Labels))
	for k, v := range r.labels {
		labels[k] = v
	}
	for k, v := range r.monitoring.Labels {
		labels[k] = v
	}

	endpoint := monitoringv1.PodMetricsEndpoint{
		Path: "/metrics",
		Port: ptr.To("http"),
	}
	if r.monitoring.ScrapeInterval != "" {
		endpoint.Interval = monitoringv1.Duration(r.monitoring.ScrapeInterval)
	}

	return &monitoringv1.PodMonitor{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "monitoring.coreos.com/v1",
			Kind:       "PodMonitor",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        r.pipeline.Name,
			Namespace:   r.pipeline.Namespace,
			Labels:      labels,
			Annotations: r.annotations(),
		},
		Spec: monitoringv1.PodMonitorSpec{
			PodMetricsEndpoints: []monitoringv1.PodMetricsEndpoint{endpoint},
			Selector: metav1.LabelSelector{
				MatchLabels: r.labels,
			},
		},
	}
}

// buildValueSourceEnv projects each spec.valueSources entry into a single
// container env var. Unlike the earlier `envFrom: secretRef` bag-of-Secrets
// pattern, every value is a named pull: the user explicitly chooses which
// key flows into which env var, and Secret keys not listed in valueSources
// stay out of the pod's environment.
func buildValueSourceEnv(pipeline *redpandav1alpha2.Pipeline) []corev1.EnvVar {
	if len(pipeline.Spec.ValueSources) == 0 {
		return []corev1.EnvVar{}
	}

	env := make([]corev1.EnvVar, 0, len(pipeline.Spec.ValueSources))
	for _, vs := range pipeline.Spec.ValueSources {
		entry := corev1.EnvVar{Name: vs.Name}
		switch {
		case vs.Source.Inline != nil:
			entry.Value = *vs.Source.Inline
		case vs.Source.SecretKeyRef != nil:
			entry.ValueFrom = &corev1.EnvVarSource{SecretKeyRef: vs.Source.SecretKeyRef}
		case vs.Source.ConfigMapKeyRef != nil:
			entry.ValueFrom = &corev1.EnvVarSource{ConfigMapKeyRef: vs.Source.ConfigMapKeyRef}
		case vs.Source.ExternalSecretRefSelector != nil:
			// External secrets are projected through a Kubernetes Secret
			// the ESO operator manages; reference it by name with the
			// same key the source declared (defaulting to the entry name).
			entry.ValueFrom = &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: vs.Source.ExternalSecretRefSelector.Name,
					},
					Key: vs.Name,
				},
			}
		default:
			// CEL on ValueSource enforces exactly-one. Skip if somehow
			// none set; the spec-level webhook would have rejected this.
			continue
		}
		env = append(env, entry)
	}
	return env
}

// buildAffinity returns the pod affinity for the pipeline: the user-supplied
// spec.affinity (e.g. node affinity pinning a node pool), merged with an
// auto-generated zone node-affinity when spec.zones is set. The zone
// requirement is AND-ed into each node-affinity term so node-pool and zone
// constraints both apply. Returns nil when neither is configured.
func buildAffinity(connect *redpandav1alpha2.Pipeline) *corev1.Affinity {
	var affinity *corev1.Affinity
	if connect.Spec.Affinity != nil {
		affinity = connect.Spec.Affinity.DeepCopy()
	}

	if len(connect.Spec.Zones) == 0 {
		return affinity // nil, or the user's affinity verbatim
	}

	if affinity == nil {
		affinity = &corev1.Affinity{}
	}
	if affinity.NodeAffinity == nil {
		affinity.NodeAffinity = &corev1.NodeAffinity{}
	}

	zoneReq := corev1.NodeSelectorRequirement{
		Key:      zoneTopologyKey,
		Operator: corev1.NodeSelectorOpIn,
		Values:   connect.Spec.Zones,
	}

	req := affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution
	if req == nil || len(req.NodeSelectorTerms) == 0 {
		// No user-required node affinity — the zone requirement is the only term.
		affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution = &corev1.NodeSelector{
			NodeSelectorTerms: []corev1.NodeSelectorTerm{
				{MatchExpressions: []corev1.NodeSelectorRequirement{zoneReq}},
			},
		}
		return affinity
	}

	// AND the zone requirement into every existing term (terms are OR-ed, so
	// adding it to each keeps "must be in zones" true regardless of which term
	// matches the node pool).
	for i := range req.NodeSelectorTerms {
		req.NodeSelectorTerms[i].MatchExpressions = append(req.NodeSelectorTerms[i].MatchExpressions, zoneReq)
	}
	return affinity
}

// buildTopologySpreadConstraints returns the topology spread constraints for
// the pipeline. If zones are configured and no explicit constraints are
// provided, a default constraint is generated to spread pods evenly across
// the specified zones.
func buildTopologySpreadConstraints(connect *redpandav1alpha2.Pipeline, selectorLabels map[string]string) []corev1.TopologySpreadConstraint {
	if len(connect.Spec.TopologySpreadConstraints) > 0 {
		return connect.Spec.TopologySpreadConstraints
	}

	if len(connect.Spec.Zones) > 0 {
		return []corev1.TopologySpreadConstraint{
			{
				MaxSkew:           1,
				TopologyKey:       zoneTopologyKey,
				WhenUnsatisfiable: corev1.ScheduleAnyway,
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: selectorLabels,
				},
			},
		}
	}

	return nil
}

// FormatConditionMessage returns a formatted message for a condition update
// during resource reconciliation.
func FormatConditionMessage(resource, detail string) string {
	return fmt.Sprintf("%s reconciliation failed: %v", resource, detail)
}
