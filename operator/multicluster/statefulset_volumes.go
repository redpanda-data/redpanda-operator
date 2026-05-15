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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// commonMounts returns the VolumeMounts shared across all containers (TLS + SASL).
func (r *RenderState) commonMounts(pool *redpandav1alpha2.NodePool) []corev1.VolumeMount {
	var mounts []corev1.VolumeMount
	if r.Spec().Auth.IsSASLEnabled() {
		sasl := r.Spec().Auth.SASL
		if sasl.SecretRef != nil && *sasl.SecretRef != "" {
			mounts = append(mounts, corev1.VolumeMount{
				Name:      "users",
				MountPath: "/etc/secrets/users",
				ReadOnly:  true,
			})
		}
	}
	for _, name := range r.PoolSpec(pool).InUseServerCerts() {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      certServerVolumeName(name),
			MountPath: certServerMountPoint(name),
		})
	}
	for _, name := range r.PoolSpec(pool).InUseClientCerts() {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      certClientVolumeName(name),
			MountPath: certClientMountPoint(name),
		})
	}
	return mounts
}

// commonVolumes returns the Volumes shared across all containers (TLS + SASL).
func (r *RenderState) commonVolumes(pool *redpandav1alpha2.NodePool) []corev1.Volume {
	poolFullname := r.poolFullname(pool)
	var volumes []corev1.Volume
	for _, name := range r.PoolSpec(pool).InUseServerCerts() {
		volumes = append(volumes, corev1.Volume{
			Name: certServerVolumeName(name),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  r.PoolSpec(pool).TLS.CertServerSecretName(poolFullname, name),
					DefaultMode: ptr.To[int32](0o440),
				},
			},
		})
	}
	for _, name := range r.PoolSpec(pool).InUseClientCerts() {
		volumes = append(volumes, corev1.Volume{
			Name: certClientVolumeName(name),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  r.PoolSpec(pool).TLS.CertClientSecretName(poolFullname, name),
					DefaultMode: ptr.To[int32](0o440),
				},
			},
		})
	}
	if r.Spec().Auth.IsSASLEnabled() {
		sasl := r.Spec().Auth.SASL
		if sasl.SecretRef != nil && *sasl.SecretRef != "" {
			volumes = append(volumes, corev1.Volume{
				Name: "users",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: *sasl.SecretRef,
					},
				},
			})
		}
	}
	return volumes
}

// statefulSetVolumes returns the Volumes for the Redpanda StatefulSet.
func statefulSetVolumes(state *RenderState, pool *redpandav1alpha2.NodePool) []corev1.Volume {
	poolFullname := state.poolFullname(pool)
	volumes := state.commonVolumes(pool)

	volumes = append(volumes,
		corev1.Volume{
			Name: lifecycleScriptsVolumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  fmt.Sprintf("%.50s-sts-lifecycle", poolFullname),
					DefaultMode: ptr.To[int32](0o775),
				},
			},
		},
		corev1.Volume{
			Name: baseConfigVolumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: poolFullname},
				},
			},
		},
		corev1.Volume{
			Name: configVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
		corev1.Volume{
			Name: fmt.Sprintf("%.51s-configurator", poolFullname),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  fmt.Sprintf("%.51s-configurator", poolFullname),
					DefaultMode: ptr.To[int32](0o775),
				},
			},
		},
	)

	if pool.Spec.InitContainers != nil && pool.Spec.InitContainers.FSValidator.IsEnabled() {
		volumes = append(volumes, corev1.Volume{
			Name: fmt.Sprintf("%.49s-fs-validator", poolFullname),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  fmt.Sprintf("%.49s-fs-validator", poolFullname),
					DefaultMode: ptr.To[int32](0o775),
				},
			},
		})
	}

	// Data directory volume.
	volumes = append(volumes, statefulSetVolumeDataDir(state, pool))

	// Tiered storage volume.
	if vol := statefulSetVolumeTieredStorageDir(state, pool); vol != nil {
		volumes = append(volumes, *vol)
	}

	// Truststore volume (projected from ConfigMaps/Secrets).
	if vol := state.PoolSpec(pool).Listeners.TrustStoreVolume(state.PoolSpec(pool).TLS); vol != nil {
		volumes = append(volumes, *vol)
	}

	// Kube API access token volume.
	volumes = append(volumes, kubeTokenAPIVolume(serviceAccountVolumeName))

	return volumes
}

// kubeTokenAPIVolume builds a projected volume that provides the three pieces
// needed for in-pod Kubernetes API access without automounting the default SA token:
//   - ServiceAccountToken: a short-lived, auto-rotated JWT (audience-bound)
//   - ConfigMap "kube-root-ca.crt": the cluster CA for TLS verification
//   - DownwardAPI namespace: the pod's namespace for building API URLs
func kubeTokenAPIVolume(name string) corev1.Volume {
	return corev1.Volume{
		Name: name,
		VolumeSource: corev1.VolumeSource{
			Projected: &corev1.ProjectedVolumeSource{
				DefaultMode: ptr.To(corev1.ProjectedVolumeSourceDefaultMode),
				Sources: []corev1.VolumeProjection{
					{
						ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
							Path:              "token",
							ExpirationSeconds: ptr.To(int64(tokenExpirationSeconds)),
						},
					},
					{
						ConfigMap: &corev1.ConfigMapProjection{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "kube-root-ca.crt",
							},
							Items: []corev1.KeyToPath{
								{Key: "ca.crt", Path: "ca.crt"},
							},
						},
					},
					{
						DownwardAPI: &corev1.DownwardAPIProjection{
							Items: []corev1.DownwardAPIVolumeFile{
								{
									Path: "namespace",
									FieldRef: &corev1.ObjectFieldSelector{
										APIVersion: "v1",
										FieldPath:  "metadata.namespace",
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func statefulSetVolumeDataDir(state *RenderState, pool *redpandav1alpha2.NodePool) corev1.Volume {
	storage := state.PoolSpec(pool).Storage

	var source corev1.VolumeSource
	switch {
	case storage != nil && storage.PersistentVolume.IsEnabled():
		source = corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: datadirVolumeName,
			},
		}
	case storage != nil && storage.HostPath != nil && *storage.HostPath != "":
		source = corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: *storage.HostPath,
			},
		}
	default:
		source = corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		}
	}

	return corev1.Volume{
		Name:         datadirVolumeName,
		VolumeSource: source,
	}
}

// statefulSetVolumeMounts returns the VolumeMounts for the Redpanda container.
func statefulSetVolumeMounts(state *RenderState, pool *redpandav1alpha2.NodePool) []corev1.VolumeMount {
	mounts := state.commonMounts(pool)

	mounts = append(mounts,
		corev1.VolumeMount{Name: configVolumeName, MountPath: redpandaConfigMountPath},
		corev1.VolumeMount{Name: baseConfigVolumeName, MountPath: baseConfigMountPath},
		corev1.VolumeMount{Name: lifecycleScriptsVolumeName, MountPath: lifecycleScriptsMountPath},
		corev1.VolumeMount{Name: datadirVolumeName, MountPath: datadirMountPath},
		corev1.VolumeMount{Name: serviceAccountVolumeName, MountPath: defaultAPITokenMountPath, ReadOnly: true},
	)

	// Truststore mount.
	if len(state.PoolSpec(pool).Listeners.TrustStores(state.PoolSpec(pool).TLS)) > 0 {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      "truststores",
			MountPath: redpandav1alpha2.TrustStoreMountPath,
			ReadOnly:  true,
		})
	}

	// Tiered storage cache directory mount.
	mountType := state.PoolSpec(pool).TieredMountType()
	if mountType != "none" {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      state.PoolSpec(pool).TieredStorageVolumeName(),
			MountPath: state.PoolSpec(pool).TieredCacheDirectory(),
		})
	}

	return mounts
}

// statefulSetVolumeTieredStorageDir returns the tiered storage volume, or nil if
// the mount type is "none" or "persistentVolume" (PVC is handled via VolumeClaimTemplates).
func statefulSetVolumeTieredStorageDir(state *RenderState, pool *redpandav1alpha2.NodePool) *corev1.Volume {
	mountType := state.PoolSpec(pool).TieredMountType()
	volName := state.PoolSpec(pool).TieredStorageVolumeName()

	switch mountType {
	case "hostPath":
		hostPath := state.PoolSpec(pool).TieredStorageHostPath()
		return &corev1.Volume{
			Name: volName,
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: hostPath,
				},
			},
		}
	case "emptyDir":
		sizeLimit := state.PoolSpec(pool).GetTieredStorageCacheSize()
		return &corev1.Volume{
			Name: volName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{
					SizeLimit: sizeLimit,
				},
			},
		}
	default:
		// "none" or "persistentVolume" (handled via VolumeClaimTemplates).
		return nil
	}
}

func volumeClaimTemplateDatadir(state *RenderState, pool *redpandav1alpha2.NodePool) *corev1.PersistentVolumeClaim {
	storage := state.PoolSpec(pool).Storage
	if storage == nil || !storage.PersistentVolume.IsEnabled() {
		return nil
	}

	pv := storage.PersistentVolume

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: datadirVolumeName,
			Labels: map[string]string{
				labelNameKey:      labelNameValue,
				labelInstanceKey:  state.releaseName,
				labelComponentKey: labelNameValue,
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{"ReadWriteOnce"},
		},
	}

	if pv.Size != nil {
		pvc.Spec.Resources = corev1.VolumeResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceStorage: *pv.Size,
			},
		}
	}

	pvc.Spec.StorageClassName = resolveStorageClass(pv.StorageClass)

	return pvc
}

// volumeClaimTemplateTieredStorageDir returns a PVC template for the tiered storage
// cache directory when mount type is "persistentVolume", or nil otherwise.
func volumeClaimTemplateTieredStorageDir(state *RenderState, pool *redpandav1alpha2.NodePool) *corev1.PersistentVolumeClaim {
	if state.PoolSpec(pool).TieredMountType() != "persistentVolume" {
		return nil
	}

	storage := state.PoolSpec(pool).Storage
	if storage == nil || storage.Tiered == nil {
		return nil
	}

	volName := state.PoolSpec(pool).TieredStorageVolumeName()

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: volName,
			Labels: map[string]string{
				labelNameKey:      labelNameValue,
				labelInstanceKey:  state.releaseName,
				labelComponentKey: labelNameValue,
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{"ReadWriteOnce"},
		},
	}

	// Size from cloud_storage_cache_size config.
	if q := state.PoolSpec(pool).GetTieredStorageCacheSize(); q != nil {
		pvc.Spec.Resources = corev1.VolumeResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceStorage: *q,
			},
		}
	}

	if storage.Tiered.PersistentVolume != nil {
		pvc.Spec.StorageClassName = resolveStorageClass(storage.Tiered.PersistentVolume.StorageClass)
	}

	return pvc
}

// resolveStorageClass maps a storage class string pointer to the PVC convention:
//   - nil or empty → nil (use cluster default)
//   - "-" → pointer to "" (explicitly no storage class, binds only to classless PVs)
//   - anything else → used as-is
func resolveStorageClass(sc *string) *string {
	if sc == nil || *sc == "" {
		return nil
	}
	if *sc == "-" {
		return ptr.To("")
	}
	return sc
}
