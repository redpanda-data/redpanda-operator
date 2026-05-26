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

// commonMounts returns the VolumeMounts shared across all containers (TLS + SASL)
// for the given pool. TLS cert mounts come from the pool's in-use cert set;
// SASL stays cluster-wide via Auth.
func (r *RenderState) commonMounts(pool *redpandav1alpha2.RedpandaBrokerPool) []corev1.VolumeMount {
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
	for _, name := range pool.Spec.InUseServerCerts() {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      certServerVolumeName(name),
			MountPath: certServerMountPoint(name),
		})
	}
	for _, name := range pool.Spec.InUseClientCerts() {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      certClientVolumeName(name),
			MountPath: certClientMountPoint(name),
		})
	}
	return mounts
}

// commonVolumes returns the Volumes shared across all containers (TLS + SASL)
// for the given pool. TLS volumes reference per-pool Secret names (keyed on
// the pool fullname so two pools using cert name "default" don't collide);
// SASL stays cluster-wide via Auth.
func (r *RenderState) commonVolumes(pool *redpandav1alpha2.RedpandaBrokerPool) []corev1.Volume {
	poolFullname := r.poolFullname(pool)
	var volumes []corev1.Volume
	for _, name := range pool.Spec.InUseServerCerts() {
		volumes = append(volumes, corev1.Volume{
			Name: certServerVolumeName(name),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  pool.Spec.TLS.CertServerSecretName(poolFullname, name),
					DefaultMode: ptr.To[int32](0o440),
				},
			},
		})
	}
	for _, name := range pool.Spec.InUseClientCerts() {
		volumes = append(volumes, corev1.Volume{
			Name: certClientVolumeName(name),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  pool.Spec.TLS.CertClientSecretName(poolFullname, name),
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
func statefulSetVolumes(state *RenderState, pool *redpandav1alpha2.RedpandaBrokerPool) []corev1.Volume {
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
	volumes = append(volumes, statefulSetVolumeDataDir(pool))

	// Tiered storage volume.
	if vol := statefulSetVolumeTieredStorageDir(pool); vol != nil {
		volumes = append(volumes, *vol)
	}

	// Truststore volume (projected from ConfigMaps/Secrets).
	if vol := pool.Spec.Listeners.TrustStoreVolume(pool.Spec.TLS); vol != nil {
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

func statefulSetVolumeDataDir(pool *redpandav1alpha2.RedpandaBrokerPool) corev1.Volume {
	storage := pool.Spec.Storage

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
func statefulSetVolumeMounts(state *RenderState, pool *redpandav1alpha2.RedpandaBrokerPool) []corev1.VolumeMount {
	mounts := state.commonMounts(pool)

	mounts = append(mounts,
		corev1.VolumeMount{Name: configVolumeName, MountPath: redpandaConfigMountPath},
		corev1.VolumeMount{Name: baseConfigVolumeName, MountPath: baseConfigMountPath},
		corev1.VolumeMount{Name: lifecycleScriptsVolumeName, MountPath: lifecycleScriptsMountPath},
		corev1.VolumeMount{Name: datadirVolumeName, MountPath: datadirMountPath},
		corev1.VolumeMount{Name: serviceAccountVolumeName, MountPath: defaultAPITokenMountPath, ReadOnly: true},
	)

	// Truststore mount.
	if len(pool.Spec.Listeners.TrustStores(pool.Spec.TLS)) > 0 {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      "truststores",
			MountPath: redpandav1alpha2.TrustStoreMountPath,
			ReadOnly:  true,
		})
	}

	// Tiered storage cache directory mount.
	mountType := pool.Spec.TieredMountType()
	if mountType != "none" {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      pool.Spec.TieredStorageVolumeName(),
			MountPath: pool.Spec.TieredCacheDirectory(),
		})
	}

	return mounts
}

// statefulSetVolumeTieredStorageDir returns the tiered storage volume, or nil if
// the mount type is "none" or "persistentVolume" (PVC is handled via VolumeClaimTemplates).
func statefulSetVolumeTieredStorageDir(pool *redpandav1alpha2.RedpandaBrokerPool) *corev1.Volume {
	mountType := pool.Spec.TieredMountType()
	volName := pool.Spec.TieredStorageVolumeName()

	switch mountType {
	case "hostPath":
		hostPath := pool.Spec.TieredStorageHostPath()
		return &corev1.Volume{
			Name: volName,
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: hostPath,
				},
			},
		}
	case "emptyDir":
		sizeLimit := pool.Spec.GetTieredStorageCacheSize()
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

func volumeClaimTemplateDatadir(state *RenderState, pool *redpandav1alpha2.RedpandaBrokerPool) *corev1.PersistentVolumeClaim {
	storage := pool.Spec.Storage
	if storage == nil || !storage.PersistentVolume.IsEnabled() {
		return nil
	}

	pv := storage.PersistentVolume

	// Operator-managed labels are written first; user labels from
	// pv.Labels then layer in via mergeStringMapFrom, which keeps the
	// operator's keys authoritative and adds user-only keys. User
	// annotations on pv.Annotations are propagated as-is. Both maps
	// arrive already merged across cluster + pool by MergeFromCluster
	// (per-key merge: pool's keys win, cluster fills the rest).
	labels := map[string]string{
		labelNameKey:      labelNameValue,
		labelInstanceKey:  state.releaseName,
		labelComponentKey: labelNameValue,
	}
	labels = mergeStringMapFrom(labels, pv.Labels)

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:        datadirVolumeName,
			Labels:      labels,
			Annotations: copyStringMap(pv.Annotations),
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
func volumeClaimTemplateTieredStorageDir(state *RenderState, pool *redpandav1alpha2.RedpandaBrokerPool) *corev1.PersistentVolumeClaim {
	if pool.Spec.TieredMountType() != "persistentVolume" {
		return nil
	}

	storage := pool.Spec.Storage
	if storage == nil || storage.Tiered == nil {
		return nil
	}

	volName := pool.Spec.TieredStorageVolumeName()

	labels := map[string]string{
		labelNameKey:      labelNameValue,
		labelInstanceKey:  state.releaseName,
		labelComponentKey: labelNameValue,
	}
	var annotations map[string]string
	if tpv := storage.Tiered.PersistentVolume; tpv != nil {
		labels = mergeStringMapFrom(labels, tpv.Labels)
		annotations = copyStringMap(tpv.Annotations)
	}

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:        volName,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{"ReadWriteOnce"},
		},
	}

	// Size from cloud_storage_cache_size config.
	if q := pool.Spec.GetTieredStorageCacheSize(); q != nil {
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

// mergeStringMapFrom returns a map where dst's keys win and src fills any
// keys not already present. Used to layer user-provided labels onto the
// operator's authoritative label set without letting users overwrite the
// operator's keys.
func mergeStringMapFrom(dst, src map[string]string) map[string]string {
	if len(src) == 0 {
		return dst
	}
	if dst == nil {
		dst = map[string]string{}
	}
	for k, v := range src {
		if _, ok := dst[k]; !ok {
			dst[k] = v
		}
	}
	return dst
}

// copyStringMap returns a shallow copy of m, or nil if m is empty/nil. Used
// for annotations to avoid aliasing the spec's map.
func copyStringMap(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
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
