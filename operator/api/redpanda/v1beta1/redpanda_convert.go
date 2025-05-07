package v1beta1

import (
	"fmt"

	"github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	corev1 "k8s.io/api/core/v1"

	// v1 "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

func (dst *Redpanda) ConvertFrom(srcRaw conversion.Hub) error {
	// Only the ClusterSpec has parallels. all other fields (ChartRef,
	// Migration) are ignored.
	src := srcRaw.(*v1alpha2.Redpanda)
	dst.ObjectMeta = src.ObjectMeta

	// // Nothing to convert if there's no ClusterSpec.
	// if src.Spec.ClusterSpec == nil {
	// 	return nil
	// }

	// We'll perform conversions based off the merged values.
	// This way we don't have to do as much nil coalescing and we can move the defaults from being implicit to explicit.
	values, err := src.GetValues()
	if err != nil {
		return err
	}

	// For completeness and clarity, ALL fields of ClusterSpec are referenced
	// in this function.
	// If a field is NOT used / supported, it will be assigned to _.

	// This function is homomorphic AFTER the first set of calls to ConvertFrom and ConvertTo.

	// Fields that are no longer supported but are present for storage
	// backwards compat.
	_ = src.Spec.ChartRef
	_ = src.Spec.ClusterSpec.NameOverride
	_ = src.Spec.ClusterSpec.FullnameOverride
	_ = src.Spec.ClusterSpec.Monitoring
	_ = src.Spec.ClusterSpec.Force
	_ = src.Spec.ClusterSpec.Tests
	_ = src.Spec.ClusterSpec.LicenseKey
	_ = src.Spec.ClusterSpec.LicenseSecretRef
	_ = src.Spec.ClusterSpec.PostInstallJob
	_ = src.Spec.ClusterSpec.PostUpgradeJob
	_ = src.Spec.ClusterSpec.Connectors

	// Fields that need conversion routines:

	// dst.Spec.NodePoolSpec.BrokerTemplate.PodTemplate = &PodTemplate{
	// 	PodTemplateApplyConfiguration: &v1.PodTemplateApplyConfiguration{},
	// }

	_ = src.Spec.ClusterSpec.CommonLabels
	_ = src.Spec.ClusterSpec.NodeSelector
	_ = src.Spec.ClusterSpec.Tolerations
	_ = src.Spec.ClusterSpec.ImagePullSecrets
	_ = src.Spec.ClusterSpec.Affinity

	_ = src.Spec.ClusterSpec.Statefulset
	_ = src.Spec.ClusterSpec.Image
	_ = src.Spec.ClusterSpec.Enterprise
	_ = src.Spec.ClusterSpec.RackAwareness
	_ = src.Spec.ClusterSpec.Console
	_ = src.Spec.ClusterSpec.Auth
	_ = src.Spec.ClusterSpec.TLS
	_ = src.Spec.ClusterSpec.External
	_ = src.Spec.ClusterSpec.Logging
	_ = src.Spec.ClusterSpec.AuditLogging
	_ = src.Spec.ClusterSpec.Resources
	_ = src.Spec.ClusterSpec.Service
	_ = src.Spec.ClusterSpec.Storage
	_ = src.Spec.ClusterSpec.Tuning
	_ = src.Spec.ClusterSpec.Listeners
	_ = src.Spec.ClusterSpec.Config
	_ = src.Spec.ClusterSpec.RBAC
	_ = src.Spec.ClusterSpec.ServiceAccount

	dst.Spec = RedpandaSpec{
		ClusterDomain: src.Spec.ClusterSpec.ClusterDomain,
		Enterprise:    convertFrom(src.Spec.ClusterSpec.Enterprise).(Enterprise),
		// ClusterConfig: values.Config.Cluster,

		NodePoolSpec: NodePoolSpec{
			Replicas: ptr.To(values.Statefulset.Replicas),
			BrokerTemplate: BrokerTemplate{
				Image:     fmt.Sprintf("%s:%s", values.Image.Repository, values.Image.Tag), // TODO this might need some explicit defaulting.
				Resources: values.Resources.GetResourceRequirements(),
				// NodeConfig: values.Config.Node,
				// RPKConfig:  values.Config.RPK,
			},
		},
	}

	return nil
}

func (src *Redpanda) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1alpha2.Redpanda)

	dst.ObjectMeta = src.ObjectMeta

	dst.Spec = v1alpha2.RedpandaSpec{
		ClusterSpec: &v1alpha2.RedpandaClusterSpec{
			ClusterDomain: src.Spec.ClusterDomain,
			Statefulset: &v1alpha2.Statefulset{
				Replicas: src.Spec.NodePoolSpec.Replicas,
			},
			Enterprise: convertTo(src.Spec.Enterprise).(*v1alpha2.Enterprise),
			Config:     &v1alpha2.Config{
				// Cluster:
			},
		},
	}

	return nil
}

// convertFrom converts sub-structs of a [v1alpha2.Redpanda] into their equivalent [v1beta1.Redpanda] sub-structs.
func convertFrom(src any) any {
	switch src := src.(type) {
	case *v1alpha2.Enterprise:
		if src == nil || (src.License == nil && src.LicenseSecretRef == nil) {
			return Enterprise{}
		}

		var valueFrom *LicenseValueFrom
		if src.LicenseSecretRef != nil {
			valueFrom = &LicenseValueFrom{
				SecretKeyRef: corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: ptr.Deref(src.LicenseSecretRef.Name, ""),
					},
					Key: ptr.Deref(src.LicenseSecretRef.Key, ""),
				},
			}
		}

		return Enterprise{
			License: &License{
				Value:     ptr.Deref(src.License, ""),
				ValueFrom: valueFrom,
			},
		}

	default:
		panic(fmt.Sprintf("unhandled typed: %T", src))
	}
}

// convertFrom converts sub-structs of a [v1beta1.Redpanda] into their equivalent [v1alpha2.Redpanda] sub-structs.
func convertTo(src any) any {
	switch src := src.(type) {
	case Enterprise:
		if src.License == nil {
			return &v1alpha2.Enterprise{}
		}

		var ref *v1alpha2.EnterpriseLicenseSecretRef
		if src.License.ValueFrom != nil {
			ref = &v1alpha2.EnterpriseLicenseSecretRef{
				Name: &src.License.ValueFrom.SecretKeyRef.Name,
				Key:  &src.License.ValueFrom.SecretKeyRef.Key,
			}
		}

		return &v1alpha2.Enterprise{
			License:          &src.License.Value,
			LicenseSecretRef: ref,
		}

	default:
		panic(fmt.Sprintf("unhandled typed: %T", src))
	}
}
