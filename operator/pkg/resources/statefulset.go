// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/go-logr/logr"
	"github.com/redpanda-data/redpanda/src/go/rpk/pkg/config"
	"gopkg.in/yaml.v3"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	redpanda "github.com/redpanda-data/redpanda-operator/charts/redpanda/v5/client"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	adminutils "github.com/redpanda-data/redpanda-operator/operator/pkg/admin"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/clusterconfiguration"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/labels"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources/featuregates"
	resourcetypes "github.com/redpanda-data/redpanda-operator/operator/pkg/resources/types"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/utils"
)

var _ Resource = &StatefulSetResource{}

var errNodePortMissing = errors.New("the node port is missing from the service")

const (
	redpandaContainerName     = "redpanda"
	configuratorContainerName = "redpanda-configurator"
	rpkStatusContainerName    = "rpk-status"

	userID  = 101
	groupID = 101
	fsGroup = 101

	configDestinationDir = "/etc/redpanda"
	configSourceDir      = "/mnt/operator"
	configFile           = "redpanda.yaml"

	scriptMountPath = "/scripts"

	DatadirName                  = "datadir"
	ArchivalCacheIndexAnchorName = "shadow-index-cache"
	defaultDatadirCapacity       = "100Gi"
	trueString                   = "true"

	// PodAnnotationNodeIDKey is identical to its label counterpart.
	PodAnnotationNodeIDKey = "operator.redpanda.com/node-id"
)

var (
	// ConfigMapHashAnnotationKey contains the hash of the node local properties of the cluster
	ConfigMapHashAnnotationKey = vectorizedv1alpha1.GroupVersion.Group + "/configmap-hash"
	// CentralizedConfigurationHashAnnotationKey contains the hash of the centralized configuration properties that require a restart when changed
	CentralizedConfigurationHashAnnotationKey = vectorizedv1alpha1.GroupVersion.Group + "/centralized-configuration-hash"

	// terminationGracePeriodSeconds should account for additional delay introduced by hooks
	terminationGracePeriodSeconds int64 = 120
)

// ConfiguratorSettings holds settings related to configurator container and deployment
// strategy
type ConfiguratorSettings struct {
	ConfiguratorBaseImage string
	ConfiguratorTag       string
	ImagePullPolicy       corev1.PullPolicy

	CloudSecretsEnabled          bool
	CloudSecretsPrefix           string
	CloudSecretsAWSRegion        string
	CloudSecretsAWSRoleARN       string
	CloudSecretsGCPProjectID     string
	CloudSecretsAzureKeyVaultURI string
}

// StatefulSetResource is part of the reconciliation of redpanda.vectorized.io CRD
// focusing on the management of redpanda cluster
type StatefulSetResource struct {
	k8sclient.Client
	scheme                 *runtime.Scheme
	pandaCluster           *vectorizedv1alpha1.Cluster
	serviceFQDN            string
	serviceName            string
	nodePortName           types.NamespacedName
	nodePortSvc            corev1.Service
	volumeProvider         resourcetypes.StatefulsetTLSVolumeProvider
	adminTLSConfigProvider resourcetypes.AdminTLSConfigProvider
	serviceAccountName     string
	configuratorSettings   ConfiguratorSettings
	// The configuration object is pushed in in order to pull out hashes of configuration.
	cfg                      *clusterconfiguration.CombinedCfg
	adminAPIClientFactory    adminutils.NodePoolAdminAPIClientFactory
	decommissionWaitInterval time.Duration
	logger                   logr.Logger
	metricsTimeout           time.Duration

	LastObservedState *appsv1.StatefulSet
	nodePool          vectorizedv1alpha1.NodePoolSpecWithDeleted

	autoDeletePVCs bool
	dialer         redpanda.DialContextFunc
}

func (r *StatefulSetResource) GetNodePool() *vectorizedv1alpha1.NodePoolSpecWithDeleted {
	return &r.nodePool
}

// NewStatefulSet creates StatefulSetResource
func NewStatefulSet(
	client k8sclient.Client,
	pandaCluster *vectorizedv1alpha1.Cluster,
	scheme *runtime.Scheme,
	serviceFQDN string,
	serviceName string,
	nodePortName types.NamespacedName,
	volumeProvider resourcetypes.StatefulsetTLSVolumeProvider,
	adminTLSConfigProvider resourcetypes.AdminTLSConfigProvider,
	serviceAccountName string,
	configuratorSettings ConfiguratorSettings,
	cfg *clusterconfiguration.CombinedCfg,
	adminAPIClientFactory adminutils.NodePoolAdminAPIClientFactory,
	dialer redpanda.DialContextFunc,
	decommissionWaitInterval time.Duration,
	logger logr.Logger,
	metricsTimeout time.Duration,
	nodePool vectorizedv1alpha1.NodePoolSpecWithDeleted, //nolint:gocritic // we want to pass by value
	autoDeletePVCs bool,
) *StatefulSetResource {
	ssr := &StatefulSetResource{
		Client:                   client,
		scheme:                   scheme,
		pandaCluster:             pandaCluster,
		serviceFQDN:              serviceFQDN,
		serviceName:              serviceName,
		nodePortName:             nodePortName,
		nodePortSvc:              corev1.Service{},
		volumeProvider:           volumeProvider,
		adminTLSConfigProvider:   adminTLSConfigProvider,
		serviceAccountName:       serviceAccountName,
		configuratorSettings:     configuratorSettings,
		cfg:                      cfg,
		adminAPIClientFactory:    adminAPIClientFactory,
		dialer:                   dialer,
		decommissionWaitInterval: decommissionWaitInterval,
		logger:                   logger.WithName("StatefulSetResource"),
		metricsTimeout:           defaultAdminAPITimeout,
		LastObservedState:        nil,
		nodePool:                 nodePool,
		autoDeletePVCs:           autoDeletePVCs,
	}

	if metricsTimeout != 0 {
		ssr.metricsTimeout = metricsTimeout
	}
	return ssr
}

// Ensure will manage kubernetes v1.StatefulSet for redpanda.vectorized.io custom resource
func (r *StatefulSetResource) Ensure(ctx context.Context) error {
	log := r.logger.WithName("StatefulSetResource.Ensure").WithValues("nodepool", r.nodePool.Name)
	log.Info("Ensure")
	if r.pandaCluster.ExternalListener() != nil {
		err := r.Get(ctx, r.nodePortName, &r.nodePortSvc)
		if err != nil {
			return fmt.Errorf("failed to retrieve node port service %s: %w", r.nodePortName, err)
		}

		for _, port := range r.nodePortSvc.Spec.Ports {
			if port.NodePort == 0 {
				return fmt.Errorf("node port service %s, port %s is 0: %w", r.nodePortName, port.Name, errNodePortMissing)
			}
		}
	}

	obj, err := r.obj(ctx)
	if err != nil {
		return fmt.Errorf("unable to construct StatefulSet object: %w", err)
	}

	created, err := CreateIfNotExists(ctx, r, obj, r.logger)
	if err != nil {
		return err
	}
	if created {
		log.Info("created StatefulSet")
		r.LastObservedState = obj.(*appsv1.StatefulSet)
		return nil
	}

	var sts appsv1.StatefulSet
	err = r.Get(ctx, r.Key(), &sts)
	if err != nil {
		return fmt.Errorf("error while fetching StatefulSet resource: %w", err)
	}
	r.LastObservedState = &sts

	// Hack for: https://github.com/redpanda-data/redpanda/issues/4999
	err = r.disableMaintenanceModeOnDecommissionedNodes(ctx)
	if err != nil {
		return err
	}

	log.Info("Running update")
	err = r.runUpdate(ctx, &sts, obj.(*appsv1.StatefulSet))
	if err != nil {
		return err
	}

	log.Info("Running scale handler")
	if err := r.handleScaling(ctx); err != nil {
		return err
	}

	// Delete StatefulSets of deleted NodePools if the conditions are met.
	// Scale handler is green, we're past its code block.
	// this STS has no outstanding work to do.
	npStatus := r.pandaCluster.Status.NodePools[r.nodePool.Name]
	if r.nodePool.Deleted && npStatus.Replicas == 0 && npStatus.ReadyReplicas == 0 && npStatus.CurrentReplicas == 0 {
		if err := r.Client.Delete(ctx, obj); err != nil {
			return fmt.Errorf("failed to delete removed StatefulSet: %w", err)
		}
		log.Info("Deleted StatefulSet")

		delete(r.pandaCluster.Status.NodePools, r.nodePool.Name)
	}

	return nil
}

// GetCentralizedConfigurationHashFromCluster retrieves the current centralized configuration hash from the statefulset
func (r *StatefulSetResource) GetCentralizedConfigurationHashFromCluster(
	ctx context.Context,
) (string, error) {
	existing := appsv1.StatefulSet{}
	if err := r.Client.Get(ctx, r.Key(), &existing); err != nil {
		return "", fmt.Errorf("could not load statefulset for reading the centralized configuration hash: %w", err)
	}
	if hash, ok := existing.Spec.Template.Annotations[CentralizedConfigurationHashAnnotationKey]; ok {
		return hash, nil
	}
	return "", nil
}

// SetCentralizedConfigurationHashInCluster saves the given centralized configuration hash in the statefulset
func (r *StatefulSetResource) SetCentralizedConfigurationHashInCluster(
	ctx context.Context, hash string,
) error {
	existing := appsv1.StatefulSet{}
	if err := r.Client.Get(ctx, r.Key(), &existing); err != nil {
		if apierrors.IsNotFound(err) {
			// No place where to store it
			return nil
		}
		return fmt.Errorf("could not load statefulset for storing the centralized configuration hash: %w", err)
	}
	if existing.Spec.Template.Annotations == nil {
		existing.Spec.Template.Annotations = make(map[string]string)
	}
	existing.Spec.Template.Annotations[CentralizedConfigurationHashAnnotationKey] = hash
	return r.Update(ctx, &existing)
}

func preparePVCResource(
	name, namespace string,
	storage vectorizedv1alpha1.StorageSpec,
	clusterLabels map[string]string,
) corev1.PersistentVolumeClaim {
	fileSystemMode := corev1.PersistentVolumeFilesystem

	pvc := corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels:    clusterLabels,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(defaultDatadirCapacity),
				},
			},
			VolumeMode: &fileSystemMode,
		},
	}

	if storage.Capacity.Value() != 0 {
		pvc.Spec.Resources.Requests[corev1.ResourceStorage] = storage.Capacity
	}

	if storage.StorageClassName != "" {
		pvc.Spec.StorageClassName = &storage.StorageClassName
	}
	return pvc
}

// obj returns resource managed client.Object
//
//nolint:funlen // The complexity of obj function will be address in the next version
func (r *StatefulSetResource) obj(
	ctx context.Context,
) (k8sclient.Object, error) {
	clusterLabels := labels.ForCluster(r.pandaCluster)
	nodePoolLabels := labels.ForCluster(r.pandaCluster).WithNodePool(r.nodePool.Name)

	annotations := r.pandaCluster.Spec.Annotations
	if annotations == nil {
		annotations = map[string]string{}
	}
	configMapHash, err := r.cfg.GetNodeConfigHash(ctx)
	if err != nil {
		return nil, err
	}
	annotations[ConfigMapHashAnnotationKey] = configMapHash

	if len(r.pandaCluster.Spec.Configuration.KafkaAPI) == 0 {
		// TODO: Fix this
		return nil, nil
	}

	externalListener := r.pandaCluster.ExternalListener()
	externalSubdomain := ""
	externalAddressType := ""
	externalEndpointTemplate := ""
	if externalListener != nil {
		externalSubdomain = externalListener.External.Subdomain
		externalAddressType = externalListener.External.PreferredAddressType
		externalEndpointTemplate = externalListener.External.EndpointTemplate
	}

	externalPandaProxyAPI := r.pandaCluster.PandaproxyAPIExternal()
	externalPandaProxyEndpointTemplate := ""
	if externalPandaProxyAPI != nil {
		externalPandaProxyEndpointTemplate = externalPandaProxyAPI.External.EndpointTemplate
	}

	tlsVolumes, tlsVolumeMounts := r.volumeProvider.Volumes()

	rpkFlags := []string{}
	u := fmt.Sprintf("%s://${POD_NAME}.%s:%d", r.pandaCluster.AdminAPIInternal().GetHTTPScheme(), r.serviceFQDN, r.pandaCluster.AdminAPIInternal().GetPort())
	rpkFlags = append(rpkFlags, fmt.Sprintf("--api-urls %q", u))
	if r.pandaCluster.AdminAPIInternal().GetTLS().Enabled {
		rpkFlags = append(rpkFlags,
			"--admin-api-tls-enabled",
			fmt.Sprintf("--admin-api-tls-truststore %q", path.Join(resourcetypes.GetTLSMountPoints().AdminAPI.NodeCertMountDir, "ca.crt")))
	}
	if r.pandaCluster.AdminAPIInternal().GetTLS().RequireClientAuth {
		rpkFlags = append(rpkFlags,
			fmt.Sprintf("--admin-api-tls-cert %q", path.Join(resourcetypes.GetTLSMountPoints().AdminAPI.ClientCAMountDir, "tls.crt")),
			fmt.Sprintf("--admin-api-tls-key %q", path.Join(resourcetypes.GetTLSMountPoints().AdminAPI.ClientCAMountDir, "tls.key")))
	}

	// In any case, configure PersistentVolumeClaimRetentionPolicy
	// Default to old behavior: Retain PVC
	// If auto-remove-pvcs flag is set, active new behavior: switch to Delete for both WhenScaled and WhenDeleted.
	var pvcReclaimRetentionPolicy *appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy
	if r.autoDeletePVCs {
		pvcReclaimRetentionPolicy = &appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy{
			WhenDeleted: appsv1.DeletePersistentVolumeClaimRetentionPolicyType,
			WhenScaled:  appsv1.DeletePersistentVolumeClaimRetentionPolicyType,
		}
	}

	nps := r.getNodePoolStatus()
	replicas := nps.CurrentReplicas

	tolerations := r.nodePool.Tolerations
	nodeSelector := r.nodePool.NodeSelector
	resLimits := r.nodePool.Resources.Limits
	resRequests := r.nodePool.Resources.Requests
	rpResources := corev1.ResourceRequirements{
		Limits:   resLimits,
		Requests: resRequests,
	}

	var canAddNodePoolToSelector bool
	{
		// Only add nodePool to selector, if all pods have the label already.
		// So long, the "old" default nodePool STS will select all pods, or STS
		// will stop recognizing the pods. If it does not recognize the pods, it
		// will refuse to create new pods (some STS-specific behavior if
		// PodManagementPolicy is Parallel)
		// We may want to guard against adding nodePools (in webhook - TODO) as long as this condition is not yet fulfilled.
		// 1. List all pods associated to the cluster
		// 2. Check if there's any one without labels.NodePoolKey
		var podList corev1.PodList
		err = r.List(ctx, &podList, &k8sclient.ListOptions{
			Namespace:     r.pandaCluster.Namespace,
			LabelSelector: clusterLabels.AsClientSelector(),
		})
		if err != nil {
			return nil, fmt.Errorf("unable to list pods: %w", err)
		}

		var atLeastOnePodMissingNodePoolKey bool
		for i := range podList.Items {
			pod := podList.Items[i]
			if _, ok := pod.GetObjectMeta().GetLabels()[labels.NodePoolKey]; !ok {
				atLeastOnePodMissingNodePoolKey = true
			}
		}

		canAddNodePoolToSelector = r.nodePool.Name != vectorizedv1alpha1.DefaultNodePoolName || !atLeastOnePodMissingNodePoolKey
	}

	var nodePoolSelector *metav1.LabelSelector
	if canAddNodePoolToSelector {
		nodePoolSelector = nodePoolLabels.AsAPISelectorForNodePool()
	} else {
		nodePoolSelector = clusterLabels.AsAPISelector()
	}

	nodePoolSpecJSON, err := json.Marshal(r.nodePool.NodePoolSpec)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal NodePoolSpec as JSON: %w", err)
	}

	ss := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: r.Key().Namespace,
			Name:      r.Key().Name,
			Labels:    nodePoolLabels,
			Annotations: map[string]string{
				labels.NodePoolSpecKey: string(nodePoolSpecJSON),
			},
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       "StatefulSet",
			APIVersion: "apps/v1",
		},
		Spec: appsv1.StatefulSetSpec{
			PersistentVolumeClaimRetentionPolicy: pvcReclaimRetentionPolicy,
			Replicas:                             &replicas,
			PodManagementPolicy:                  appsv1.ParallelPodManagement,
			Selector:                             nodePoolSelector,
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.OnDeleteStatefulSetStrategyType,
			},
			ServiceName: r.pandaCluster.Name,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:        r.pandaCluster.Name,
					Namespace:   r.pandaCluster.Namespace,
					Labels:      nodePoolLabels,
					Annotations: annotations,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: r.getServiceAccountName(),
					SecurityContext: &corev1.PodSecurityContext{
						FSGroup: ptr.To(int64(fsGroup)),
					},
					PriorityClassName: r.pandaCluster.Spec.PriorityClassName,
					Volumes: append([]corev1.Volume{
						{
							Name: "configmap-dir",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: ConfigMapKey(r.pandaCluster).Name,
									},
								},
							},
						},
						{
							Name: "config-dir",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
						{
							Name: "hook-scripts-dir",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName:  SecretKey(r.pandaCluster).Name,
									DefaultMode: ptr.To(int32(0o555)),
								},
							},
						},
					}, tlsVolumes...),
					TerminationGracePeriodSeconds: &terminationGracePeriodSeconds,
					InitContainers: []corev1.Container{
						{
							Name:            configuratorContainerName,
							Image:           r.fullConfiguratorImage(),
							Command:         []string{"/redpanda-operator"},
							Args:            r.getConfiguratorArgs(),
							ImagePullPolicy: r.configuratorSettings.ImagePullPolicy,
							Env: append([]corev1.EnvVar{
								{
									Name:  "HOST_INDEX_OFFSET",
									Value: strconv.Itoa(r.nodePool.NodePoolSpec.HostIndexOffset),
								},
								{
									Name:  "SERVICE_FQDN",
									Value: r.serviceFQDN,
								},
								{
									Name:  "CONFIG_SOURCE_DIR",
									Value: configSourceDir,
								},
								{
									Name:  "CONFIG_DESTINATION",
									Value: filepath.Join(configDestinationDir, configFile),
								},
								{
									Name:  "REDPANDA_RPC_PORT",
									Value: strconv.Itoa(r.pandaCluster.Spec.Configuration.RPCServer.Port),
								},
								{
									Name: "NODE_NAME",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											APIVersion: "v1",
											FieldPath:  "spec.nodeName",
										},
									},
								},
								{
									Name:  "EXTERNAL_CONNECTIVITY",
									Value: strconv.FormatBool(externalListener != nil),
								},
								{
									Name:  "EXTERNAL_CONNECTIVITY_SUBDOMAIN",
									Value: externalSubdomain,
								},
								{
									Name:  "EXTERNAL_CONNECTIVITY_ADDRESS_TYPE",
									Value: externalAddressType,
								},
								{
									Name:  "EXTERNAL_CONNECTIVITY_KAFKA_ENDPOINT_TEMPLATE",
									Value: externalEndpointTemplate,
								},
								{
									Name:  "EXTERNAL_CONNECTIVITY_PANDA_PROXY_ENDPOINT_TEMPLATE",
									Value: externalPandaProxyEndpointTemplate,
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
								{
									Name:  "HOST_PORT",
									Value: r.getNodePort(ExternalListenerName),
								},
								{
									Name:  "RACK_AWARENESS",
									Value: strconv.FormatBool(featuregates.RackAwareness(r.pandaCluster.Spec.Version)),
								},
								{
									Name:  "VALIDATE_MOUNTED_VOLUME",
									Value: strconv.FormatBool(r.pandaCluster.Spec.InitialValidationForVolume != nil && *r.pandaCluster.Spec.InitialValidationForVolume),
								},
							}, append(r.pandaproxyEnvVars(), r.AdditionalListenersEnvVars()...)...),
							SecurityContext: &corev1.SecurityContext{
								RunAsUser:  ptr.To(int64(userID)),
								RunAsGroup: ptr.To(int64(groupID)),
							},
							Resources: rpResources,
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "config-dir",
									MountPath: configDestinationDir,
								},
								{
									Name:      "configmap-dir",
									MountPath: configSourceDir,
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:    redpandaContainerName,
							Image:   r.pandaCluster.FullImageName(),
							Command: []string{"/usr/bin/rpk"},
							Args: append([]string{
								"redpanda",
								"start",
								"--check=false",
								r.portsConfiguration(),
							}, prepareAdditionalArguments(
								r.pandaCluster.Spec.Configuration.DeveloperMode,
								r.nodePool.Resources,
								r.nodePool.AdditionalCommandlineArguments)...),
							Env: []corev1.EnvVar{
								{
									Name:  "REDPANDA_ENVIRONMENT",
									Value: "kubernetes",
								},
								{
									Name: "POD_NAME",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											APIVersion: "v1",
											FieldPath:  "metadata.name",
										},
									},
								},
								{
									Name: "POD_NAMESPACE",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											APIVersion: "v1",
											FieldPath:  "metadata.namespace",
										},
									},
								},
								{
									Name: "POD_IP",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											APIVersion: "v1",
											FieldPath:  "status.podIP",
										},
									},
								},
							},
							Ports: append([]corev1.ContainerPort{
								{
									Name:          "rpc",
									ContainerPort: int32(r.pandaCluster.Spec.Configuration.RPCServer.Port),
								},
							}, r.getPorts()...),
							ReadinessProbe: &corev1.Probe{
								TimeoutSeconds: 5,
								ProbeHandler: corev1.ProbeHandler{
									Exec: &corev1.ExecAction{
										Command: []string{"bash", "-xc", fmt.Sprintf("rpk cluster health %s| grep 'Healthy:.*true'", strings.Join(rpkFlags, " "))},
									},
								},
							},
							SecurityContext: &corev1.SecurityContext{
								RunAsUser:  ptr.To(int64(userID)),
								RunAsGroup: ptr.To(int64(groupID)),
							},
							Resources: rpResources,
							VolumeMounts: append([]corev1.VolumeMount{
								{
									Name:      "config-dir",
									MountPath: configDestinationDir,
								},
								{
									Name:      "hook-scripts-dir",
									MountPath: scriptMountPath,
								},
							}, tlsVolumeMounts...),
						},
					},
					Tolerations:  tolerations,
					NodeSelector: nodeSelector,
					Affinity: &corev1.Affinity{
						PodAntiAffinity: &corev1.PodAntiAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
								{
									LabelSelector: clusterLabels.AsAPISelector(),
									Namespaces:    []string{r.pandaCluster.Namespace},
									TopologyKey:   corev1.LabelHostname,
								},
							},
							PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
								{
									Weight: 100,
									PodAffinityTerm: corev1.PodAffinityTerm{
										LabelSelector: clusterLabels.AsAPISelector(),
										Namespaces:    []string{r.pandaCluster.Namespace},
										TopologyKey:   corev1.LabelHostname,
									},
								},
							},
						},
					},
					TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
						{
							MaxSkew:           1,
							TopologyKey:       corev1.LabelZoneFailureDomainStable,
							WhenUnsatisfiable: corev1.ScheduleAnyway,
							LabelSelector:     clusterLabels.AsAPISelector(),
						},
					},
				},
			},
		},
	}

	// Only multi-replica clusters should use maintenance mode. See: https://github.com/redpanda-data/redpanda/issues/4338
	// Startup of a fresh cluster would let the first pod restart, until dynamic hooks are implemented. See: https://github.com/redpanda-data/redpanda/pull/4907
	multiReplica := r.pandaCluster.CalculateCurrentReplicas() > 1

	if featuregates.MaintenanceMode(r.pandaCluster.Spec.Version) && r.pandaCluster.IsUsingMaintenanceModeHooks() && multiReplica {
		ss.Spec.Template.Spec.Containers[0].Lifecycle = &corev1.Lifecycle{
			PreStop:   r.getHook(preStopKey),
			PostStart: r.getHook(postStartKey),
		}
	}

	okToPatch, err := r.canOverwriteBootstrapForAnyPreexistingResource(ctx, ss)
	if err != nil {
		return nil, err
	}
	if okToPatch {
		// Bootstrap configuration is now templated; we don't need to mount the original here
		ss.Spec.Template.Spec.InitContainers[0].Env = append(ss.Spec.Template.Spec.InitContainers[0].Env,
			corev1.EnvVar{
				Name:  bootstrapTemplateEnvVar,
				Value: filepath.Join(configSourceDir, bootstrapTemplateFile),
			},
			corev1.EnvVar{
				Name:  bootstrapDestinationEnvVar,
				Value: filepath.Join(configDestinationDir, bootstrapConfigFile),
			},
		)
		// If the bootstrap template needs additional env vars, inject them here
		additionalEnv, err := r.cfg.AdditionalInitEnvVars()
		if err != nil {
			return nil, err
		}
		ss.Spec.Template.Spec.InitContainers[0].Env = append(ss.Spec.Template.Spec.InitContainers[0].Env, additionalEnv...)
	} else {
		// TODO: drop this. It'll trigger a restart when the operator moves, but as per discussions that's considered acceptable.
		// Keep the old .bootstrap.yaml in place.
		ss.Spec.Template.Spec.Containers[0].VolumeMounts = append(ss.Spec.Template.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
			Name:      "configmap-dir",
			MountPath: path.Join(configDestinationDir, bootstrapConfigFile),
			SubPath:   bootstrapConfigFile,
		})
	}

	setVolumes(ss, r.pandaCluster, r.nodePool.Storage, r.nodePool.CloudCacheStorage)

	rpkStatusContainer := r.rpkStatusContainer(tlsVolumeMounts)
	if rpkStatusContainer != nil {
		ss.Spec.Template.Spec.Containers = append(ss.Spec.Template.Spec.Containers, *rpkStatusContainer)
	}

	err = controllerutil.SetControllerReference(r.pandaCluster, ss, r.scheme)
	if err != nil {
		return nil, err
	}

	return ss, nil
}

// checkForPreexistingResource will check if the resource already exists.
// If it does, we may want to avoid patching it to avoid a superfluous restart.
func (r *StatefulSetResource) canOverwriteBootstrapForAnyPreexistingResource(ctx context.Context, sts *appsv1.StatefulSet) (bool, error) {
	// Attempt to fetch the resource
	var oldObj appsv1.StatefulSet
	err := r.Get(ctx, types.NamespacedName{
		Namespace: sts.Namespace,
		Name:      sts.Name,
	}, &oldObj)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// We should create the object fully patched, since it doesn't exist
			return true, nil
		}
		return false, err
	}
	// Check to see if we have the signature env variable in the initContainer;
	// if so we're good to continue.
	if len(oldObj.Spec.Template.Spec.InitContainers) > 0 {
		for _, ev := range oldObj.Spec.Template.Spec.InitContainers[0].Env {
			if ev.Name == bootstrapTemplateEnvVar {
				// This already has the hooks; it's fine to patch.
				return true, nil
			}
		}
		// Check to see if we are changing any images; if so, we can patch.
		if sts.Spec.Template.Spec.InitContainers[0].Image != r.fullConfiguratorImage() {
			return true, nil
		}
	}
	if len(sts.Spec.Template.Spec.Containers) > 0 && sts.Spec.Template.Spec.Containers[0].Image != r.pandaCluster.FullImageName() {
		return true, nil
	}
	// Otherwise, we'll leave this untouched for the moment; it's already running,
	// and so the bootstrap configuration is irrelevant.
	return false, nil
}

func (r *StatefulSetResource) getConfiguratorArgs() []string {
	result := []string{"configure"}
	if r.configuratorSettings.CloudSecretsEnabled {
		result = append(result, "--enable-cloud-secrets=true")
		result = append(result, fmt.Sprintf("--cloud-secrets-prefix=%s", r.configuratorSettings.CloudSecretsPrefix))
		if r.configuratorSettings.CloudSecretsAWSRegion != "" {
			result = append(result, fmt.Sprintf("--cloud-secrets-aws-region=%s", r.configuratorSettings.CloudSecretsAWSRegion))
		}
		if r.configuratorSettings.CloudSecretsAWSRoleARN != "" {
			result = append(result, fmt.Sprintf("--cloud-secrets-aws-role-arn=%s", r.configuratorSettings.CloudSecretsAWSRoleARN))
		}
		if r.configuratorSettings.CloudSecretsGCPProjectID != "" {
			result = append(result, fmt.Sprintf("--cloud-secrets-gcp-project-id=%s", r.configuratorSettings.CloudSecretsGCPProjectID))
		}
		if r.configuratorSettings.CloudSecretsAzureKeyVaultURI != "" {
			result = append(result, fmt.Sprintf("--cloud-secrets-azure-key-vault-ur=%s", r.configuratorSettings.CloudSecretsAzureKeyVaultURI))
		}
	}
	return result
}

// getPrestopHook creates a hook that drains the node before shutting down.
func (r *StatefulSetResource) getHook(script string) *corev1.LifecycleHandler {
	return &corev1.LifecycleHandler{
		Exec: &corev1.ExecAction{
			Command: []string{
				scriptMountPath + "/" + script,
			},
		},
	}
}

// setVolumes manipulates v1.StatefulSet object in order to add cloud storage and
// Redpanda data volume
func setVolumes(ss *appsv1.StatefulSet, cluster *vectorizedv1alpha1.Cluster, data, cache vectorizedv1alpha1.StorageSpec) {
	pvcDataDir := preparePVCResource(DatadirName, cluster.Namespace, data, ss.Labels)
	ss.Spec.VolumeClaimTemplates = append(ss.Spec.VolumeClaimTemplates, pvcDataDir)
	vol := corev1.Volume{
		Name: DatadirName,
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: DatadirName,
			},
		},
	}
	ss.Spec.Template.Spec.Volumes = append(ss.Spec.Template.Spec.Volumes, vol)

	containers := ss.Spec.Template.Spec.Containers
	for i := range containers {
		if containers[i].Name == redpandaContainerName {
			volMount := corev1.VolumeMount{
				Name:      DatadirName,
				MountPath: dataDirectory,
			}
			containers[i].VolumeMounts = append(containers[i].VolumeMounts, volMount)
		}
	}

	initContainer := ss.Spec.Template.Spec.InitContainers
	for i := range initContainer {
		if initContainer[i].Name == configuratorContainerName {
			volMount := corev1.VolumeMount{
				Name:      DatadirName,
				MountPath: dataDirectory,
			}
			initContainer[i].VolumeMounts = append(initContainer[i].VolumeMounts, volMount)
		}
	}

	if cluster.Spec.CloudStorage.Enabled && featuregates.ShadowIndex(cluster.Spec.Version) {
		pvcArchivalDir := preparePVCResource(ArchivalCacheIndexAnchorName, cluster.Namespace, cache, ss.Labels)
		ss.Spec.VolumeClaimTemplates = append(ss.Spec.VolumeClaimTemplates, pvcArchivalDir)
		archivalVol := corev1.Volume{
			Name: ArchivalCacheIndexAnchorName,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: ArchivalCacheIndexAnchorName,
				},
			},
		}
		ss.Spec.Template.Spec.Volumes = append(ss.Spec.Template.Spec.Volumes, archivalVol)

		for i := range containers {
			if containers[i].Name == redpandaContainerName {
				archivalVolMount := corev1.VolumeMount{
					Name:      ArchivalCacheIndexAnchorName,
					MountPath: archivalCacheIndexDirectory,
				}
				containers[i].VolumeMounts = append(containers[i].VolumeMounts, archivalVolMount)
			}
		}
	}
}

func (r *StatefulSetResource) rpkStatusContainer(
	tlsVolumeMounts []corev1.VolumeMount,
) *corev1.Container {
	if r.pandaCluster.Spec.Sidecars.RpkStatus == nil || !r.pandaCluster.Spec.Sidecars.RpkStatus.Enabled {
		return nil
	}
	return &corev1.Container{
		Name:    rpkStatusContainerName,
		Image:   r.pandaCluster.FullImageName(),
		Command: []string{"/usr/local/bin/rpk-status.sh"},
		Env: []corev1.EnvVar{
			{
				Name:  "REDPANDA_ENVIRONMENT",
				Value: "kubernetes",
			},
		},
		Resources: *r.pandaCluster.Spec.Sidecars.RpkStatus.Resources,
		VolumeMounts: append([]corev1.VolumeMount{
			{
				Name:      "config-dir",
				MountPath: configDestinationDir,
			},
		}, tlsVolumeMounts...),
	}
}

func prepareAdditionalArguments(
	developerMode bool,
	originalRequests vectorizedv1alpha1.RedpandaResourceRequirements,
	additionalCommandlineArguments map[string]string,
) []string {
	requests := originalRequests.DeepCopy()

	requestedCores := requests.RedpandaCPU().Value()
	requestedMemory := requests.RedpandaMemory().Value()

	args := make(map[string]string)
	if developerMode {
		args["overprovisioned"] = ""
		args["kernel-page-cache"] = trueString
		args["default-log-level"] = "debug"
	} else {
		args["default-log-level"] = "info"
	}

	// When cpu is not set, all cores are used
	if requestedCores > 0 {
		args["smp"] = strconv.FormatInt(requestedCores, 10)
	}

	// When memory is not set, all of the host memory is used minus max(1.5Gi, 7%)
	if requestedMemory > 0 {
		// Both of these flags shouldn't be set at the same time:
		// https://github.com/scylladb/seastar/issues/375
		//
		// However, this allows explicitly setting the amount of memory to
		// the required value, and the code in seastar hasn't changed in
		// years.
		//
		// The correct way to do it is to set just --reserve-memory
		// taking into account:
		// * Seastar sees the total host memory
		// * k8s has an allocatable amount of memory
		// * DefaultRequestBaseMemory reservation
		// * Memory buffer for the cgroup
		//
		// All of which doesn't feel much less fragile or intuitive.
		args["memory"] = strconv.FormatInt(requestedMemory, 10)
		args["reserve-memory"] = "0M"
	}

	for k, v := range additionalCommandlineArguments {
		args[k] = v
	}

	out := make([]string, 0)
	for k, v := range args {
		if v == "" {
			out = append(out, fmt.Sprintf("--%s", k))
		} else {
			out = append(out, fmt.Sprintf("--%s=%s", k, v))
		}
	}
	sort.Strings(out)
	return out
}

// TODO: lift this into configuration construction
func (r *StatefulSetResource) pandaproxyEnvVars() []corev1.EnvVar {
	var envs []corev1.EnvVar
	listener := r.pandaCluster.PandaproxyAPIExternal()
	if listener != nil {
		envs = append(envs, corev1.EnvVar{
			Name:  "PROXY_HOST_PORT",
			Value: r.getNodePort(PandaproxyPortExternalName),
		})
	}
	return envs
}

func (r *StatefulSetResource) getNodePort(name string) string {
	for _, port := range r.nodePortSvc.Spec.Ports {
		if port.Name == name {
			return strconv.FormatInt(int64(port.NodePort), 10)
		}
	}
	return ""
}

func (r *StatefulSetResource) getServiceAccountName() string {
	return r.serviceAccountName
}

// Key returns namespace/name object that is used to identify object.
// For reference please visit types.NamespacedName docs in k8s.io/apimachinery
func (r *StatefulSetResource) Key() types.NamespacedName {
	var name string
	if strings.EqualFold(r.nodePool.Name, vectorizedv1alpha1.DefaultNodePoolName) {
		name = r.pandaCluster.Name
	} else {
		name = fmt.Sprintf("%s-%s", r.pandaCluster.Name, r.nodePool.Name)
	}

	return types.NamespacedName{Name: name, Namespace: r.pandaCluster.Namespace}
}

func (r *StatefulSetResource) portsConfiguration() string {
	rpcAPIPort := r.pandaCluster.Spec.Configuration.RPCServer.Port
	serviceFQDN := r.serviceFQDN

	return fmt.Sprintf("--advertise-rpc-addr=$(POD_NAME).%s:%d", serviceFQDN, rpcAPIPort)
}

func (r *StatefulSetResource) getPorts() []corev1.ContainerPort {
	ports := []corev1.ContainerPort{{
		Name:          AdminPortName,
		ContainerPort: int32(r.pandaCluster.AdminAPIInternal().Port),
	}}
	internalListener := r.pandaCluster.InternalListener()
	ports = append(ports, corev1.ContainerPort{
		Name:          InternalListenerName,
		ContainerPort: int32(internalListener.Port),
	})
	if internalProxy := r.pandaCluster.PandaproxyAPIInternal(); internalProxy != nil {
		ports = append(ports, corev1.ContainerPort{
			Name:          PandaproxyPortInternalName,
			ContainerPort: int32(internalProxy.Port),
		})
	}

	if r.pandaCluster.Spec.Configuration.SchemaRegistry != nil &&
		r.pandaCluster.Spec.Configuration.SchemaRegistry.External != nil &&
		!r.pandaCluster.Spec.Configuration.SchemaRegistry.External.Enabled {
		ports = append(ports, corev1.ContainerPort{
			Name:          SchemaRegistryPortName,
			ContainerPort: int32(r.pandaCluster.Spec.Configuration.SchemaRegistry.Port),
		})
	}

	ports = append(ports, r.GetPortsForListenersInAdditionalConfig()...)

	if len(r.nodePortSvc.Spec.Ports) > 0 {
		for _, port := range r.nodePortSvc.Spec.Ports {
			ports = append(ports, corev1.ContainerPort{
				Name: port.Name,
				// To distinguish external from internal clients the new listener
				// and port is exposed for Redpanda clients. The port is chosen
				// arbitrary to the KafkaAPI + 1, because user can not reach this
				// port. The routing in the Kubernetes will forward all traffic from
				// HostPort to the ContainerPort.
				ContainerPort: port.TargetPort.IntVal,
				// The host port is set to the service node port that doesn't have
				// any endpoints.
				HostPort: port.NodePort,
			})
		}
		return ports
	}

	return ports
}

func (r *StatefulSetResource) AdditionalKafkaExternalListeners() (advertisedlisteners []listenerTemplateSpec) {
	listeners := r.pandaCluster.KafkaAPIExternalListeners()
	for _, l := range listeners {
		if l.Name == DefaultExternalKafkaListenerName {
			continue
		}
		port := strconv.Itoa(l.Port)
		advertisedPort := port
		if l.External.PortTemplate != "" {
			advertisedPort = l.External.PortTemplate
		}
		advertisedlisteners = append(advertisedlisteners, listenerTemplateSpec{
			Name:    l.Name,
			Address: fmt.Sprintf("%s.%s", l.External.EndpointTemplate, l.External.Subdomain),
			Port:    TemplatedInt(advertisedPort),
		})
	}
	return advertisedlisteners
}

func (r *StatefulSetResource) AdditionalPandaProxyExternalListeners() (advertisedlisteners []listenerTemplateSpec) {
	listeners := r.pandaCluster.PandaproxyAPIExternalListeners()
	for _, l := range listeners {
		if l.Name == DefaultExternalProxyListenerName {
			continue
		}
		port := strconv.Itoa(l.Port)
		advertisedPort := port
		if l.External.PortTemplate != "" {
			advertisedPort = l.External.PortTemplate
		}
		advertisedlisteners = append(advertisedlisteners, listenerTemplateSpec{
			Name:    l.Name,
			Address: fmt.Sprintf("%s.%s", l.External.EndpointTemplate, l.External.Subdomain),
			Port:    TemplatedInt(advertisedPort),
		})
	}
	return advertisedlisteners
}

// allAdditionalExternalListenersFromSpecAPIs returns all additional external listeners from the API blocks under
// the spec configuration, including KafkaAPI, PandaproxyAPI, and SchemaRegistryAPI.
func (r *StatefulSetResource) allAdditionalExternalListenersFromSpecAPIs() *allListenersTemplateSpec {
	kafkaAdvertisedListeners := r.AdditionalKafkaExternalListeners()
	proxyAdvertisedListeners := r.AdditionalPandaProxyExternalListeners()

	return &allListenersTemplateSpec{
		KafkaAdvertisedListeners: kafkaAdvertisedListeners,
		ProxyAdvertisedListeners: proxyAdvertisedListeners,
	}
}

// AdditionalListenersEnvVars returns the env var passed to the configurator for configuring additioanl listeners.
func (r *StatefulSetResource) AdditionalListenersEnvVars() []corev1.EnvVar {
	listenersFromAdditionalCfg := map[string]string{}
	if len(r.pandaCluster.Spec.AdditionalConfiguration) > 0 {
		for _, k := range AdditionalListenerCfgNames {
			if v, found := r.pandaCluster.Spec.AdditionalConfiguration[k]; found {
				listenersFromAdditionalCfg[k] = v
			}
		}
	}

	listeners := r.allAdditionalExternalListenersFromSpecAPIs()
	jsonStr, err := listeners.Append(listenersFromAdditionalCfg)
	if err != nil {
		r.logger.Error(err, "failed to concat additional listeners")
		return nil
	}
	envVar := "ADDITIONAL_LISTENERS_JSON"
	if listeners.IsEmpty() {
		// If no additional listeners are set in the spec APIs, we use the legacy env var
		// for backward compatibility since jsonStr is in legacy format with single quotes.
		envVar = "ADDITIONAL_LISTENERS"
	}
	return []corev1.EnvVar{{
		Name:  envVar,
		Value: jsonStr,
	}}
}

// GetPortsForListenersInAdditionalConfig gets the ports for the additional listeners and advertised APIs set in additionalConfiguration.
// - redpanda.kafka_api
// - redpanda.advertised_kafka_api
// - pandaproxy.pandaproxy_api
// - pandaproxy.advertised_pandaproxy_api
// - schema_registry.schema_registry_api
// example: redpanda.kafka_api: "[{'name':'private-link','address':'0.0.0.0','port':39002}]"
func (r *StatefulSetResource) GetPortsForListenersInAdditionalConfig() []corev1.ContainerPort {
	ports := []corev1.ContainerPort{}

	if len(r.pandaCluster.Spec.AdditionalConfiguration) == 0 {
		return ports
	}

	getPorts := func(key string) []corev1.ContainerPort {
		if v, found := r.pandaCluster.Spec.AdditionalConfiguration[key]; found {
			var ports []corev1.ContainerPort
			res, err := utils.Compute(v, utils.NewEndpointTemplateData(0, "dummy", r.nodePool.HostIndexOffset), false)
			if err != nil {
				r.logger.Error(err, "failed to execute endpoint template", "additionalProperty", key)
				return nil
			}

			var addrs []config.NamedAuthNSocketAddress
			if err := yaml.Unmarshal([]byte(res), &addrs); err != nil {
				r.logger.Error(err, "failed to unmarshal additionalProperty %s into []config.NamedAuthNSocketAddress", "additionalProperty", key)
				return nil
			}
			for _, v := range addrs {
				ports = append(ports, corev1.ContainerPort{
					Name:          getAdditionalListenerPortName(v.Name),
					ContainerPort: int32(v.Port),
				})
			}
			return ports
		}
		return nil
	}

	ports = append(ports, getPorts(KafkaAPIConfigPath)...)
	ports = append(ports, getPorts(PandaproxyAPIConfigPath)...)

	return ports
}

func (r *StatefulSetResource) fullConfiguratorImage() string {
	return fmt.Sprintf("%s:%s", r.configuratorSettings.ConfiguratorBaseImage, r.configuratorSettings.ConfiguratorTag)
}

// Version returns the cluster version specified in the image tag of the
// statefulset spec. Depending on the rollout status it might not be the same as
// version of the pods.
func (r *StatefulSetResource) Version() string {
	lastObservedSts := r.LastObservedState
	if lastObservedSts != nil {
		return redpandaContainerVersion(lastObservedSts.Spec.Template.Spec.Containers)
	}
	return ""
}

// getAdditionalListenerPortName returns the name of container port for additional listener.
// Container port name must be unique and its length can not exceed 15.
func getAdditionalListenerPortName(listenerName string) string {
	s := 0
	if len(listenerName) > 15 {
		s = len(listenerName) - 15
	}
	return listenerName[s:]
}

func redpandaContainerVersion(containers []corev1.Container) string {
	for i := range containers {
		c := containers[i]
		if c.Name != redpandaContainerName {
			continue
		}
		// Will always have tag even for latest because of pandaCluster.FullImageName().
		if s := strings.Split(c.Image, ":"); len(s) > 1 {
			version := s[len(s)-1]
			// Image uses registry with port and no tag (e.g. localhost:5000/redpanda)
			if strings.Contains(version, "/") {
				version = ""
			}
			return version
		}
	}
	return ""
}

// CurrentVersion is the version that's rolled out to all nodes (pods) of the cluster
func (r *StatefulSetResource) CurrentVersion(ctx context.Context) (string, error) {
	stsVersion := r.Version()
	if stsVersion == "" {
		return "", nil
	}
	replicas := *r.LastObservedState.Spec.Replicas

	podList, err := r.getPodList(ctx)
	if err != nil {
		return "", err
	}

	pods := podList.Items
	if int32(len(pods)) != replicas {
		//nolint:goerr113 // not going to use wrapped static error here this time
		return stsVersion, fmt.Errorf("rollout incomplete: pods count %d does not match expected replicas %d", len(pods), replicas)
	}
	for i := range pods {
		if !utils.IsPodReady(&pods[i]) {
			//nolint:goerr113 // no need for static error
			return stsVersion, fmt.Errorf("rollout incomplete: at least one pod (%s) is not READY", pods[i].Name)
		}
		podVersion := redpandaContainerVersion(pods[i].Spec.Containers)
		if podVersion != stsVersion {
			//nolint:goerr113 // no need for static error
			return stsVersion, fmt.Errorf("rollout incomplete: at least one pod has version %s not %s", podVersion, stsVersion)
		}
	}
	return stsVersion, nil
}

func (r *StatefulSetResource) IsManagedDecommission() (bool, error) {
	t, ok := r.pandaCluster.GetAnnotations()[ManagedDecommissionAnnotation]
	if !ok {
		return false, nil
	}
	deadline, err := time.Parse(time.RFC3339, t)
	if err != nil {
		return false, fmt.Errorf("managed decommission annotation must be a valid RFC3339 timestamp: %w", err)
	}
	return deadline.After(time.Now()), nil
}

func (r *StatefulSetResource) getPodByBrokerID(ctx context.Context, brokerID *int32) (*corev1.Pod, error) {
	if brokerID == nil {
		return nil, nil
	}
	brokerIDStr := strconv.FormatInt(int64(*brokerID), 10)
	pods, err := r.getPodList(ctx)
	if err != nil {
		return nil, err
	}

	return GetPodByBrokerIDfromPodList(brokerIDStr, pods), nil
}

func (r *StatefulSetResource) GetReplicas() int32 {
	return ptr.Deref(r.nodePool.Replicas, 0)
}

func GetPodByBrokerIDfromPodList(brokerIDStr string, pods *corev1.PodList) *corev1.Pod {
	for i := range pods.Items {
		annotations := pods.Items[i].GetAnnotations()
		if v, ok := annotations[PodAnnotationNodeIDKey]; ok && v == brokerIDStr {
			return &pods.Items[i]
		}
	}
	return nil
}

type KeyNotPresentError struct {
	Key string
}

func (e *KeyNotPresentError) Error() string {
	return fmt.Sprintf("key not present: %v", e.Key)
}

func (r *StatefulSetResource) getBrokerIDForPod(ctx context.Context, ordinal int32) (*int32, error) {
	stsName := r.Key().Name
	ordinalStr := strconv.FormatInt(int64(ordinal), 10)
	podName := stsName + "-" + ordinalStr

	pods, err := r.getPodList(ctx)
	if err != nil {
		return nil, err
	}
	return GetBrokerIDForPodFromPodList(pods, podName)
}

func GetBrokerIDForPodFromPodList(pods *corev1.PodList, podName string) (*int32, error) {
	for i := range pods.Items {
		if pods.Items[i].GetName() != podName {
			continue
		}
		brokerIDStr, ok := pods.Items[i].GetAnnotations()[PodAnnotationNodeIDKey]
		if !ok {
			return nil, fmt.Errorf("node-id annotation is not set on pod %s %w", podName, &KeyNotPresentError{Key: PodAnnotationNodeIDKey})
		}
		brokerIDint, err := strconv.ParseInt(brokerIDStr, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("error converting node-id, %q, from pod annotation to integer: %w", brokerIDStr, err)
		}
		brokerID := int32(brokerIDint)
		return &brokerID, nil
	}
	return nil, nil
}

func (r *StatefulSetResource) GetNodePoolPods(ctx context.Context) (*corev1.PodList, error) {
	var sts appsv1.StatefulSet
	err := r.Client.Get(ctx, r.Key(), &sts)
	if err != nil {
		return nil, fmt.Errorf("while retrieving STS for np %s: %w", r.nodePool.Name, err)
	}

	var stsPods corev1.PodList
	s, err := metav1.LabelSelectorAsSelector(sts.Spec.Selector)
	if err != nil {
		return nil, fmt.Errorf("while creating pod selector for sts %s: %w", r.nodePool.Name, err)
	}

	err = r.Client.List(ctx, &stsPods, k8sclient.MatchingLabelsSelector{Selector: s})
	if err != nil {
		return nil, fmt.Errorf("while listing pods of nodepool %s: %w", r.nodePool.Name, err)
	}

	return &stsPods, nil
}
