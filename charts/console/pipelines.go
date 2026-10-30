// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package console

import (
	"fmt"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	"github.com/redpanda-data/common-go/kube"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

const (
	// pipelineAPIAudience must match the operator's pipelineapi.DefaultAudience.
	pipelineAPIAudience  = "redpanda-pipeline-api"
	pipelineAPIPort      = 8080
	pipelineAPIPortName  = "grpc"
	pipelineAPITokenDir  = "/var/run/secrets/redpanda.com/pipeline-api"
	pipelineAPITokenPath = "/var/run/secrets/redpanda.com/pipeline-api/token"
	pipelineAPICADir     = "/etc/console/pipeline-api-ca"
	pipelineAPICAPath    = "/etc/console/pipeline-api-ca/ca.crt"
	pipelineAPITLSDir    = "/etc/pipeline-api/tls"
	// pipelineAPICADuration is the lifetime of the chart-managed CA: five
	// years, as Console reads it only at startup.
	pipelineAPICADuration = "43800h"
)

// PipelinesTypes returns the kinds that [Render] returns only when
// pipelines.enabled is true and that [Types] doesn't already list.
func PipelinesTypes() []kube.Object {
	return []kube.Object{
		&rbacv1.Role{},
		&rbacv1.RoleBinding{},
		&rbacv1.ClusterRoleBinding{},
		&networkingv1.NetworkPolicy{},
		&certmanagerv1.Issuer{},
		&certmanagerv1.Certificate{},
	}
}

// pipelinesAuthenticated reports whether Console authenticates to the
// pipeline API with its ServiceAccount token, which Consoles that predate
// cloud.redpandaConnect.tokenFile can't.
func pipelinesAuthenticated(state *RenderState) bool {
	return state.Values.Pipelines.Enabled && !state.Values.Pipelines.AllowUnauthenticated
}

func pipelinesTLS(state *RenderState) bool {
	return state.Values.Pipelines.Enabled && state.Values.Pipelines.TLS.Enabled
}

// pipelinesManagedCerts reports whether the chart issues the pipeline API's
// certificate with cert-manager, rather than using tls.secretName.
func pipelinesManagedCerts(state *RenderState) bool {
	return pipelinesTLS(state) && state.Values.Pipelines.TLS.SecretName == ""
}

// pipelineAPITLSSecretName holds tls.crt, tls.key, and the ca.crt Console
// trusts.
func pipelineAPITLSSecretName(state *RenderState) string {
	if state.Values.Pipelines.TLS.SecretName != "" {
		return state.Values.Pipelines.TLS.SecretName
	}
	return fmt.Sprintf("%s-tls", PipelineAPIName(state))
}

// PipelineAPIName names the pipeline API's namespaced objects.
func PipelineAPIName(state *RenderState) string {
	return cleanForK8s(fmt.Sprintf("%s-pipeline-api", state.FullName()))
}

// pipelineAPISelectorLabels select the pipeline API's Pods. They differ from
// [RenderState.SelectorLabels] so Console's Service and Deployment don't
// select them.
func pipelineAPISelectorLabels(state *RenderState) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      cleanForK8s(fmt.Sprintf("%s-pipeline-api", state.ChartName())),
		"app.kubernetes.io/instance":  state.ReleaseName,
		"app.kubernetes.io/component": "pipeline-api",
	}
}

// validatePipelines fails rendering for pipelines values that would leave the
// pipeline API unusable or reachable by Pods other than Console's.
func validatePipelines(state *RenderState) {
	if state.Values.Pipelines.Image.Tag == "" {
		panic("pipelines.image.tag is required: set it to the Redpanda Operator version that serves the pipeline API")
	}
	// Without a userRef, cluster-bound pipelines authenticate as the
	// cluster's bootstrap superuser.
	if state.Values.Pipelines.ClusterRef != "" && state.Values.Pipelines.UserRef == "" {
		panic("pipelines.clusterRef requires pipelines.userRef: set it to a User with ACLs scoped to Console's pipelines")
	}
	// Consoles that can't send a token can't use TLS either.
	if state.Values.Pipelines.AllowUnauthenticated && state.Values.Pipelines.TLS.Enabled {
		panic("pipelines.allowUnauthenticated is for Consoles that can't send a token, which can't use TLS either: set pipelines.tls.enabled: false with it")
	}
	// The pipeline API accepts tokens of Console's ServiceAccount, which
	// every Pod that names no ServiceAccount shares if it's the default one.
	if pipelinesAuthenticated(state) && ServiceAccountName(state) == "default" {
		panic("pipelines.enabled requires a dedicated ServiceAccount for Console: set serviceAccount.create: true or serviceAccount.name")
	}
}

// consoleConfig returns `config` with `cloud.redpandaConnect` pointed at the
// pipeline API. These keys win over the same keys in `config`.
func consoleConfig(state *RenderState) map[string]any {
	if !state.Values.Pipelines.Enabled {
		return state.Values.Config
	}

	redpandaConnect := helmette.Merge(map[string]any{}, helmette.Dig(state.Values.Config, map[string]any{}, "cloud", "redpandaConnect").(map[string]any))
	redpandaConnect["enabled"] = true
	redpandaConnect["address"] = fmt.Sprintf("%s.%s.svc:%d", PipelineAPIName(state), state.Namespace, pipelineAPIPort)
	// Consoles that predate these keys refuse to start with them.
	if pipelinesAuthenticated(state) {
		redpandaConnect["tokenFile"] = pipelineAPITokenPath
	}
	if pipelinesTLS(state) {
		redpandaConnect["tls"] = map[string]any{
			"enabled":    true,
			"caFilepath": pipelineAPICAPath,
		}
	}

	cloud := helmette.Merge(map[string]any{}, helmette.Dig(state.Values.Config, map[string]any{}, "cloud").(map[string]any))
	cloud["redpandaConnect"] = redpandaConnect

	config := helmette.Merge(map[string]any{}, state.Values.Config)
	config["cloud"] = cloud

	return config
}

// pipelineAPITokenVolume projects a token of Console's ServiceAccount, bound
// to the pipeline API's audience, into the Console Pods.
func pipelineAPITokenVolume() corev1.Volume {
	return corev1.Volume{
		Name: "pipeline-api-token",
		VolumeSource: corev1.VolumeSource{
			Projected: &corev1.ProjectedVolumeSource{
				Sources: []corev1.VolumeProjection{
					{
						ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
							Audience:          pipelineAPIAudience,
							ExpirationSeconds: ptr.To(int64(3600)),
							Path:              "token",
						},
					},
				},
			},
		},
	}
}

func pipelineAPITokenVolumeMount() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      "pipeline-api-token",
		MountPath: pipelineAPITokenDir,
		ReadOnly:  true,
	}
}

// pipelineAPICAVolume mounts only ca.crt from the pipeline API's TLS Secret
// into the Console Pods.
func pipelineAPICAVolume(state *RenderState) corev1.Volume {
	return corev1.Volume{
		Name: "pipeline-api-ca",
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: pipelineAPITLSSecretName(state),
				Items:      []corev1.KeyToPath{{Key: "ca.crt", Path: "ca.crt"}},
			},
		},
	}
}

func pipelineAPICAVolumeMount() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      "pipeline-api-ca",
		MountPath: pipelineAPICADir,
		ReadOnly:  true,
	}
}

// consolePipelinesVolumes returns the Console Pods' volumes for reaching the
// pipeline API.
func consolePipelinesVolumes(state *RenderState) []corev1.Volume {
	var volumes []corev1.Volume
	if pipelinesAuthenticated(state) {
		volumes = append(volumes, pipelineAPITokenVolume())
	}
	if pipelinesTLS(state) {
		volumes = append(volumes, pipelineAPICAVolume(state))
	}
	return volumes
}

func consolePipelinesVolumeMounts(state *RenderState) []corev1.VolumeMount {
	var mounts []corev1.VolumeMount
	if pipelinesAuthenticated(state) {
		mounts = append(mounts, pipelineAPITokenVolumeMount())
	}
	if pipelinesTLS(state) {
		mounts = append(mounts, pipelineAPICAVolumeMount())
	}
	return mounts
}

func PipelineAPIServiceAccount(state *RenderState) *corev1.ServiceAccount {
	if !state.Values.Pipelines.Enabled {
		return nil
	}

	return &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ServiceAccount",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      PipelineAPIName(state),
			Namespace: state.Namespace,
			Labels:    state.Labels(nil),
		},
	}
}

// PipelineAPIRole grants the pipeline API access to the Pipelines in Console's
// namespace.
func PipelineAPIRole(state *RenderState) *rbacv1.Role {
	if !state.Values.Pipelines.Enabled {
		return nil
	}

	return &rbacv1.Role{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "Role",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      PipelineAPIName(state),
			Namespace: state.Namespace,
			Labels:    state.Labels(nil),
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"cluster.redpanda.com"},
				Resources: []string{"pipelines"},
				Verbs:     []string{"get", "list", "create", "update", "delete"},
			},
		},
	}
}

func PipelineAPIRoleBinding(state *RenderState) *rbacv1.RoleBinding {
	if !state.Values.Pipelines.Enabled {
		return nil
	}

	return &rbacv1.RoleBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "RoleBinding",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      PipelineAPIName(state),
			Namespace: state.Namespace,
			Labels:    state.Labels(nil),
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     PipelineAPIName(state),
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      PipelineAPIName(state),
				Namespace: state.Namespace,
			},
		},
	}
}

// PipelineAPIClusterRoleBinding lets the pipeline API authenticate Console
// with TokenReview.
func PipelineAPIClusterRoleBinding(state *RenderState) *rbacv1.ClusterRoleBinding {
	if !pipelinesAuthenticated(state) {
		return nil
	}

	return &rbacv1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "ClusterRoleBinding",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   pipelinesClusterScopedName(state, "pipeline-api"),
			Labels: state.Labels(nil),
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "system:auth-delegator",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      PipelineAPIName(state),
				Namespace: state.Namespace,
			},
		},
	}
}

// PipelineAPIDeployment runs the operator's pipeline API, which serves
// Console's pipeline calls from the Pipelines in Console's namespace.
func PipelineAPIDeployment(state *RenderState) *appsv1.Deployment {
	if !state.Values.Pipelines.Enabled {
		return nil
	}

	validatePipelines(state)

	args := []string{
		"pipeline-api",
		fmt.Sprintf("--namespace=%s", state.Namespace),
		fmt.Sprintf("--listen-address=:%d", pipelineAPIPort),
	}
	if pipelinesAuthenticated(state) {
		args = append(args, fmt.Sprintf("--allowed-caller=system:serviceaccount:%s:%s", state.Namespace, ServiceAccountName(state)))
	} else {
		args = append(args, "--allow-unauthenticated")
	}

	var volumes []corev1.Volume
	var mounts []corev1.VolumeMount
	scheme := corev1.URISchemeHTTP
	if pipelinesTLS(state) {
		args = append(args,
			fmt.Sprintf("--tls-cert-file=%s/tls.crt", pipelineAPITLSDir),
			fmt.Sprintf("--tls-key-file=%s/tls.key", pipelineAPITLSDir),
		)
		volumes = append(volumes, corev1.Volume{
			Name: "tls",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: pipelineAPITLSSecretName(state),
					Items: []corev1.KeyToPath{
						{Key: "tls.crt", Path: "tls.crt"},
						{Key: "tls.key", Path: "tls.key"},
					},
				},
			},
		})
		mounts = append(mounts, corev1.VolumeMount{
			Name:      "tls",
			MountPath: pipelineAPITLSDir,
			ReadOnly:  true,
		})
		scheme = corev1.URISchemeHTTPS
	}
	if state.Values.Pipelines.ClusterRef != "" {
		args = append(args, fmt.Sprintf("--cluster-ref=%s", state.Values.Pipelines.ClusterRef))
	}
	if state.Values.Pipelines.UserRef != "" {
		args = append(args, fmt.Sprintf("--user-ref=%s", state.Values.Pipelines.UserRef))
	}

	probe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path:   "/healthz",
				Port:   intstr.FromString(pipelineAPIPortName),
				Scheme: scheme,
			},
		},
	}

	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      PipelineAPIName(state),
			Namespace: state.Namespace,
			Labels:    state.Labels(nil),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{
				MatchLabels: pipelineAPISelectorLabels(state),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: pipelineAPISelectorLabels(state),
				},
				Spec: corev1.PodSpec{
					ServiceAccountName:           PipelineAPIName(state),
					AutomountServiceAccountToken: ptr.To(true),
					ImagePullSecrets:             state.Values.ImagePullSecrets,
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: ptr.To(true),
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					Volumes: volumes,
					Containers: []corev1.Container{
						{
							Name:            "pipeline-api",
							Image:           fmt.Sprintf("%s:%s", state.Values.Pipelines.Image.Repository, state.Values.Pipelines.Image.Tag),
							ImagePullPolicy: state.Values.Pipelines.Image.PullPolicy,
							// The image's entrypoint runs the controller manager.
							Command: []string{"/redpanda-operator"},
							Args:    args,
							Ports: []corev1.ContainerPort{
								{
									Name:          pipelineAPIPortName,
									ContainerPort: pipelineAPIPort,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							VolumeMounts:   mounts,
							ReadinessProbe: probe,
							LivenessProbe:  probe,
							Resources:      state.Values.Pipelines.Resources,
							SecurityContext: &corev1.SecurityContext{
								AllowPrivilegeEscalation: ptr.To(false),
								ReadOnlyRootFilesystem:   ptr.To(true),
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{"ALL"},
								},
							},
						},
					},
				},
			},
		},
	}
}

func PipelineAPIService(state *RenderState) *corev1.Service {
	if !state.Values.Pipelines.Enabled {
		return nil
	}

	port := corev1.ServicePort{
		Name:       pipelineAPIPortName,
		Port:       pipelineAPIPort,
		TargetPort: intstr.FromString(pipelineAPIPortName),
		Protocol:   corev1.ProtocolTCP,
	}
	// kubernetes.io/h2c names plaintext HTTP/2 only.
	if !pipelinesTLS(state) {
		port.AppProtocol = ptr.To("kubernetes.io/h2c")
	}

	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      PipelineAPIName(state),
			Namespace: state.Namespace,
			Labels:    state.Labels(nil),
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: pipelineAPISelectorLabels(state),
			Ports:    []corev1.ServicePort{port},
		},
	}
}

// PipelineAPINetworkPolicy admits only Console's Pods to the pipeline API,
// which is all that guards it with pipelines.allowUnauthenticated.
func PipelineAPINetworkPolicy(state *RenderState) *networkingv1.NetworkPolicy {
	if !state.Values.Pipelines.Enabled || !state.Values.Pipelines.NetworkPolicy.Enabled {
		return nil
	}

	return &networkingv1.NetworkPolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "networking.k8s.io/v1",
			Kind:       "NetworkPolicy",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      PipelineAPIName(state),
			Namespace: state.Namespace,
			Labels:    state.Labels(nil),
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: pipelineAPISelectorLabels(state),
			},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					From: []networkingv1.NetworkPolicyPeer{
						{PodSelector: &metav1.LabelSelector{MatchLabels: state.SelectorLabels()}},
					},
					Ports: []networkingv1.NetworkPolicyPort{
						{
							Protocol: ptr.To(corev1.ProtocolTCP),
							Port:     ptr.To(intstr.FromString(pipelineAPIPortName)),
						},
					},
				},
			},
		},
	}
}

// The chart bootstraps a CA Issuer for the pipeline API as cert-manager
// documents: https://cert-manager.io/docs/configuration/selfsigned/#bootstrapping-ca-issuers

func PipelineAPISelfSignedIssuer(state *RenderState) *certmanagerv1.Issuer {
	if !pipelinesManagedCerts(state) {
		return nil
	}

	return &certmanagerv1.Issuer{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "cert-manager.io/v1",
			Kind:       "Issuer",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-selfsigned", PipelineAPIName(state)),
			Namespace: state.Namespace,
			Labels:    state.Labels(nil),
		},
		Spec: certmanagerv1.IssuerSpec{
			IssuerConfig: certmanagerv1.IssuerConfig{
				SelfSigned: &certmanagerv1.SelfSignedIssuer{},
			},
		},
	}
}

func PipelineAPICACertificate(state *RenderState) *certmanagerv1.Certificate {
	if !pipelinesManagedCerts(state) {
		return nil
	}

	return &certmanagerv1.Certificate{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "cert-manager.io/v1",
			Kind:       "Certificate",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-ca", PipelineAPIName(state)),
			Namespace: state.Namespace,
			Labels:    state.Labels(nil),
		},
		Spec: certmanagerv1.CertificateSpec{
			Duration:   helmette.MustDuration(pipelineAPICADuration),
			IsCA:       true,
			CommonName: fmt.Sprintf("%s-ca", PipelineAPIName(state)),
			SecretName: fmt.Sprintf("%s-ca", PipelineAPIName(state)),
			PrivateKey: &certmanagerv1.CertificatePrivateKey{
				Algorithm: "ECDSA",
				Size:      256,
			},
			IssuerRef: cmmetav1.ObjectReference{
				Name:  fmt.Sprintf("%s-selfsigned", PipelineAPIName(state)),
				Kind:  "Issuer",
				Group: "cert-manager.io",
			},
		},
	}
}

func PipelineAPICAIssuer(state *RenderState) *certmanagerv1.Issuer {
	if !pipelinesManagedCerts(state) {
		return nil
	}

	return &certmanagerv1.Issuer{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "cert-manager.io/v1",
			Kind:       "Issuer",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-ca", PipelineAPIName(state)),
			Namespace: state.Namespace,
			Labels:    state.Labels(nil),
		},
		Spec: certmanagerv1.IssuerSpec{
			IssuerConfig: certmanagerv1.IssuerConfig{
				CA: &certmanagerv1.CAIssuer{
					SecretName: fmt.Sprintf("%s-ca", PipelineAPIName(state)),
				},
			},
		},
	}
}

// PipelineAPICertificate is the pipeline API's serving certificate, whose
// Secret also carries the ca.crt that Console trusts.
func PipelineAPICertificate(state *RenderState) *certmanagerv1.Certificate {
	if !pipelinesManagedCerts(state) {
		return nil
	}

	name := PipelineAPIName(state)
	return &certmanagerv1.Certificate{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "cert-manager.io/v1",
			Kind:       "Certificate",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: state.Namespace,
			Labels:    state.Labels(nil),
		},
		Spec: certmanagerv1.CertificateSpec{
			CommonName: name,
			DNSNames: []string{
				name,
				fmt.Sprintf("%s.%s", name, state.Namespace),
				fmt.Sprintf("%s.%s.svc", name, state.Namespace),
			},
			SecretName: pipelineAPITLSSecretName(state),
			PrivateKey: &certmanagerv1.CertificatePrivateKey{
				Algorithm: "ECDSA",
				Size:      256,
			},
			Usages: []certmanagerv1.KeyUsage{certmanagerv1.UsageServerAuth, certmanagerv1.UsageDigitalSignature},
			IssuerRef: cmmetav1.ObjectReference{
				Name:  fmt.Sprintf("%s-ca", name),
				Kind:  "Issuer",
				Group: "cert-manager.io",
			},
		},
	}
}

// pipelinesClusterScopedName names a cluster-scoped object after a hash of the
// release's namespace and name, so that same-named releases in different
// namespaces don't collide.
func pipelinesClusterScopedName(state *RenderState, suffix string) string {
	// NB: sha256 rather than FNV, which Helm templates can't compute.
	hash := helmette.Trunc(8, helmette.Sha256Sum(fmt.Sprintf("%s/%s", state.Namespace, state.ReleaseName)))
	tail := fmt.Sprintf("-%s-%s", hash, suffix)
	return fmt.Sprintf("%s%s", helmette.TrimSuffix("-", helmette.Trunc(63-len(tail), state.FullName())), tail)
}
