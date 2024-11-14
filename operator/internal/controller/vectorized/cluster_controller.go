// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package vectorized contains reconciliation logic for redpanda.vectorized.io CRDs
package vectorized

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/fluxcd/pkg/runtime/logger"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/redpanda-data/redpanda/src/go/rpk/pkg/config"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	adminutils "github.com/redpanda-data/redpanda-operator/operator/pkg/admin"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/labels"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/networking"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/patch"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources/featuregates"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/utils"
)

const (
	FinalizerKey = "operator.redpanda.com/finalizer"

	SecretAnnotationExternalCAKey = "operator.redpanda.com/external-ca"

	NotManaged = "false"

	managedPath = "/managed"
)

var (
	errNonexistentLastObservedState = errors.New("expecting to have statefulset LastObservedState set but it's nil")
	errNodePortMissing              = errors.New("the node port is missing from the service")
	errInvalidImagePullPolicy       = errors.New("invalid image pull policy")
)

// ClusterReconciler reconciles a Cluster object
type ClusterReconciler struct {
	client.Client
	Log                       logr.Logger
	configuratorSettings      resources.ConfiguratorSettings
	clusterDomain             string
	Scheme                    *runtime.Scheme
	AdminAPIClientFactory     adminutils.NodePoolAdminAPIClientFactory
	DecommissionWaitInterval  time.Duration
	MetricsTimeout            time.Duration
	RestrictToRedpandaVersion string
	GhostDecommissioning      bool
	AutoDeletePVCs            bool
}

//+kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cert-manager.io,resources=issuers;certificates,verbs=create;get;list;watch;patch;delete;update;
//+kubebuilder:rbac:groups=cert-manager.io,resources=clusterissuers,verbs=create;get;list;watch;patch;delete;update;
//+kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=get;list;watch;delete;
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;update;delete;patch
// Permission on pods/finalizers may or may not be required
//+kubebuilder:rbac:groups=core,resources=pods/finalizers,verbs=update;patch
//+kubebuilder:rbac:groups=core,resources=pods/status,verbs=update;patch
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;
//+kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;create;update;patch;
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;
//+kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=create;get;list;watch;patch;delete;update;
//+kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=create;get;list;watch;patch;delete;update;
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings,verbs=get;list;watch;create;update;patch;
//+kubebuilder:rbac:groups=scheduling.k8s.io,resources=priorityclasses,verbs=get;list;watch
//+kubebuilder:rbac:groups=redpanda.vectorized.io,resources=clusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=redpanda.vectorized.io,resources=clusters/finalizers,verbs=update;patch
//+kubebuilder:rbac:groups=redpanda.vectorized.io,resources=clusters/status,verbs=get;update;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// the Cluster object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.7.0/pkg/reconcile
//
//nolint:funlen,gocyclo // todo break down
func (r *ClusterReconciler) Reconcile(
	c context.Context, req ctrl.Request,
) (result ctrl.Result, err error) {
	ctx, done := context.WithCancel(c)
	defer done()
	log := ctrl.LoggerFrom(ctx).WithName("ClusterReconciler.Reconcile")

	log.Info("Starting reconcile loop")
	defer log.Info("Finished reconcile loop")

	var vectorizedCluster vectorizedv1alpha1.Cluster
	ar := newAttachedResources(ctx, r, log, &vectorizedCluster)
	if err := r.Get(ctx, req.NamespacedName, &vectorizedCluster); err != nil {
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		if apierrors.IsNotFound(err) {
			if removeError := ar.getClusterRoleBinding().RemoveSubject(ctx, req.NamespacedName); removeError != nil {
				return ctrl.Result{}, fmt.Errorf("unable to remove subject in ClusterroleBinding: %w", removeError)
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("unable to retrieve Cluster resource: %w", err)
	}

	// After every reconciliation, update status:
	// - Set observedGeneration. The reconciler finished, every action
	//   performed in this run - including updating status - has been finished, and has
	//   observed this generation.
	// - Set OperatorQuiescent condition, based on our best knowledge if there is
	//   any outstanding work to do for the controller.
	defer func() {
		_, patchErr := patch.PatchStatus(ctx, r.Client, &vectorizedCluster, func(cluster *vectorizedv1alpha1.Cluster) {
			// Set quiescent
			cond := getQuiescentCondition(cluster)

			flipped := cluster.Status.SetCondition(cond.Type, cond.Status, cond.Reason, cond.Message)
			if flipped {
				log.Info("Changing OperatorQuiescent condition after reconciliation", "status", cond.Status, "reason", cond.Reason, "message", cond.Message)
			}

			// Only set observedGeneration if there's no error.
			if err == nil {
				cluster.Status.ObservedGeneration = vectorizedCluster.Generation
			}
		})
		if patchErr != nil {
			log.Error(patchErr, "failed to patchStatus with observedGeneration and quiescent")
		}
	}()

	// Previous usage of finalizer handlers was unreliable in the case of
	// flipping Kubernetes Nodes ready status. Local SSD disks that could be
	// attached to Redpanda Pod prevents rescheduling as the Persistent Volume
	// affinity bounds Pod to only one Node. In case of Kubernetes Node coming
	// back to live Cluster controller could already delete Redpanda data (PVC
	// deletion and Redpanda decommissioning). If particular Redpanda Node would
	// host single replica partition, then it would be a data lost.
	//
	// If the majority of Redpanda process would run in unstable Kubernetes
	// Nodes, then Redpanda operator could break whole cluster by losing Raft
	// quorum.
	if err := r.removeFinalizers(ctx, &vectorizedCluster, log); err != nil {
		return ctrl.Result{}, err
	}

	isManaged := isRedpandaClusterManaged(log, &vectorizedCluster) && isRedpandaClusterVersionManaged(log, &vectorizedCluster, r.RestrictToRedpandaVersion)
	if !isManaged {
		return ctrl.Result{}, nil
	}

	// Expect featuregate to be supported. Clusters <22.3.0 are unsupported.
	if !featuregates.EmptySeedStartCluster(vectorizedCluster.Spec.Version) {
		return ctrl.Result{}, fmt.Errorf("Redpanda version >=v22.3.0 is required to support FeatureGate EmptySeedStartCluster")
	}

	ar.bootstrapService()
	ar.clusterRole()
	ar.clusterRoleBinding()
	ar.clusterService()
	ar.headlessService()
	ar.ingress()
	ar.nodeportService()
	if err := ar.pki(); err != nil {
		return ctrl.Result{}, err
	}
	pki, err := ar.getPKI()
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("getting pki: %w", err)
	}
	ar.podDisruptionBudget()
	ar.proxySuperuser()
	ar.schemaRegistrySuperUser()
	ar.serviceAccount()
	ar.secret()

	var secrets []types.NamespacedName
	if ar.getProxySuperuser() != nil {
		secrets = append(secrets, ar.getProxySuperUserKey())
	}
	if ar.getSchemaRegistrySuperUser() != nil {
		secrets = append(secrets, ar.getSchemaRegistrySuperUserKey())
	}

	if err = ar.configMap(); err != nil {
		return ctrl.Result{}, fmt.Errorf("creating configmap: %w", err)
	}
	cm, err := ar.getConfigMap()
	if err != nil {
		return ctrl.Result{}, err
	}
	if err = ar.statefulSet(); err != nil {
		return ctrl.Result{}, fmt.Errorf("creating statefulsets: %w", err)
	}

	if err = r.setPodNodeIDAnnotation(ctx, &vectorizedCluster, log, ar); err != nil {
		// Setting NodeID annotations is best-effort. Log the error, but don't fail because of it.
		log.Error(err, "failed to set node_id annotation")
	}

	result, errs := ar.Ensure()
	if !result.IsZero() && errs == nil {
		return result, nil
	}
	if errs != nil {
		return result, errs
	}

	adminAPI, err := r.AdminAPIClientFactory(ctx, r.Client, &vectorizedCluster, ar.getHeadlessServiceFQDN(), pki.AdminAPIConfigProvider())
	if err != nil && !errors.Is(err, &adminutils.NoInternalAdminAPI{}) {
		return ctrl.Result{}, fmt.Errorf("creating admin api client: %w", err)
	}

	if errSetInit := r.setInitialSuperUserPassword(ctx, adminAPI, secrets); errSetInit != nil {
		// we capture all errors here, do not return the error, just requeue
		log.Error(errSetInit, "failed to set initial super user password")
		return ctrl.Result{RequeueAfter: resources.RequeueDuration}, nil
	}

	schemaRegistryPort := config.DefaultSchemaRegPort
	if vectorizedCluster.Spec.Configuration.SchemaRegistry != nil {
		schemaRegistryPort = vectorizedCluster.Spec.Configuration.SchemaRegistry.Port
	}
	stSets, err := ar.getStatefulSet()
	if err != nil {
		return ctrl.Result{}, err
	}

	err = r.reportStatus(
		ctx,
		&vectorizedCluster,
		stSets,
		ar.getHeadlessServiceFQDN(),
		ar.getClusterServiceFQDN(),
		schemaRegistryPort,
		ar.getNodeportServiceKey(),
		ar.getBootstrapServiceKey(),
	)
	if err != nil {
		return ctrl.Result{}, err
	}

	err = r.reconcileConfiguration(
		ctx,
		&vectorizedCluster,
		cm,
		stSets,
		pki,
		ar.getHeadlessServiceFQDN(),
		log,
	)
	var requeueErr *resources.RequeueAfterError
	if errors.As(err, &requeueErr) {
		log.Info(requeueErr.Error())
		return ctrl.Result{RequeueAfter: requeueErr.RequeueAfter}, nil
	}
	if err != nil {
		return ctrl.Result{}, err
	}

	if featuregates.CentralizedConfiguration(vectorizedCluster.Spec.Version) {
		if cc := vectorizedCluster.Status.GetCondition(vectorizedv1alpha1.ClusterConfiguredConditionType); cc == nil || cc.Status != corev1.ConditionTrue {
			return ctrl.Result{RequeueAfter: time.Minute * 1}, nil
		}
	}

	// The following should be at the last part as it requires AdminAPI to be running
	if err := r.setPodNodeIDAnnotation(ctx, &vectorizedCluster, log, ar); err != nil {
		log.Error(err, "setting pod node_id annotation after reconciling resources")
		return ctrl.Result{
			RequeueAfter: 4 * time.Second,
		}, nil
	}

	if err := r.setPodNodeIDLabel(ctx, &vectorizedCluster, log, ar); err != nil {
		return ctrl.Result{}, fmt.Errorf("setting pod node_id label: %w", err)
	}

	// want: refactor above to resources (i.e. setInitialSuperUserPassword, reconcileConfiguration)
	// ensuring license must be at the end when condition ClusterConfigured=true and AdminAPI is ready
	license := resources.NewLicense(r.Client, r.Scheme, &vectorizedCluster, adminAPI, log)
	if err := license.Ensure(ctx); err != nil {
		var raErr *resources.RequeueAfterError
		if errors.As(err, &raErr) {
			log.Info(raErr.Error())
			return ctrl.Result{RequeueAfter: raErr.RequeueAfter}, nil
		}
		return ctrl.Result{}, err
	}

	if r.GhostDecommissioning {
		r.decommissionGhostBrokers(ctx, &vectorizedCluster, log, ar)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := validateImagePullPolicy(r.configuratorSettings.ImagePullPolicy); err != nil {
		return fmt.Errorf("invalid image pull policy \"%s\": %w", r.configuratorSettings.ImagePullPolicy, err)
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&vectorizedv1alpha1.Cluster{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.Service{}).
		Watches(
			&corev1.Pod{},
			handler.EnqueueRequestsFromMapFunc(r.reconcileClusterForPods),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.reconcileClusterForExternalCASecret),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Complete(r)
}

func validateImagePullPolicy(imagePullPolicy corev1.PullPolicy) error {
	switch imagePullPolicy {
	case corev1.PullAlways:
	case corev1.PullIfNotPresent:
	case corev1.PullNever:
	default:
		return fmt.Errorf("available image pull policy: \"%s\", \"%s\" or \"%s\": %w", corev1.PullAlways, corev1.PullIfNotPresent, corev1.PullNever, errInvalidImagePullPolicy)
	}
	return nil
}

func (r *ClusterReconciler) removePodFinalizer(
	ctx context.Context, pod *corev1.Pod, l logr.Logger,
) error {
	log := l.WithName("removePodFinalizer")
	if controllerutil.ContainsFinalizer(pod, FinalizerKey) {
		log.V(logger.DebugLevel).WithValues("namespace", pod.Namespace, "name", pod.Name).Info("removing finalizer")
		p := client.MergeFrom(pod.DeepCopy())
		controllerutil.RemoveFinalizer(pod, FinalizerKey)
		if err := r.Patch(ctx, pod, p); err != nil {
			return fmt.Errorf("unable to remove pod (%s/%s) finalizer: %w", pod.Namespace, pod.Name, err)
		}
	}
	return nil
}

func (r *ClusterReconciler) setPodNodeIDAnnotation(
	ctx context.Context, rp *vectorizedv1alpha1.Cluster, l logr.Logger, ar *attachedResources,
) error {
	log := l.WithName("setPodNodeIDAnnotation")
	log.V(logger.DebugLevel).Info("setting pod node-id annotation")
	pods, err := r.podList(ctx, rp)
	if err != nil {
		return fmt.Errorf("unable to fetch PodList: %w", err)
	}

	var combinedErrors error
	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.Annotations == nil {
			pod.Annotations = make(map[string]string)
		}
		nodeIDStrAnnotation, annotationExist := pod.Annotations[resources.PodAnnotationNodeIDKey]

		nodeID, err := r.fetchAdminNodeID(ctx, rp, pod, ar)
		if err != nil {
			combinedErrors = errors.Join(combinedErrors, fmt.Errorf(`cannot fetch node id for "%s" node-id annotation: %w`, pod.Name, err))
			continue
		}

		realNodeIDStr := fmt.Sprintf("%d", nodeID)

		if annotationExist && realNodeIDStr == nodeIDStrAnnotation {
			continue
		}

		log.WithValues("pod-name", pod.Name, "new-node-id", nodeID).Info("setting node-id annotation")
		pod.Annotations[resources.PodAnnotationNodeIDKey] = realNodeIDStr
		if err := r.Update(ctx, pod, &client.UpdateOptions{}); err != nil {
			combinedErrors = errors.Join(combinedErrors, fmt.Errorf(`unable to update pod "%s" with node-id annotation: %w`, pod.Name, err))
			continue
		}
	}
	return combinedErrors
}

func (r *ClusterReconciler) setPodNodeIDLabel(
	ctx context.Context, rp *vectorizedv1alpha1.Cluster, l logr.Logger, ar *attachedResources,
) error {
	log := l.WithName("setPodNodeIDLabel")
	log.V(logger.DebugLevel).Info("setting pod node-id label")
	pods, err := r.podList(ctx, rp)
	if err != nil {
		return fmt.Errorf("unable to fetch PodList: %w", err)
	}
	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.Labels == nil {
			pod.Labels = make(map[string]string)
		}
		nodeIDStrLabel, labelExist := pod.Labels[resources.PodAnnotationNodeIDKey]

		nodeID, err := r.fetchAdminNodeID(ctx, rp, pod, ar)
		if err != nil {
			log.Error(err, `cannot fetch node id for node-id annotation`)
			continue
		}

		realNodeIDStr := fmt.Sprintf("%d", nodeID)

		if labelExist && realNodeIDStr == nodeIDStrLabel {
			continue
		}

		log.WithValues("pod-name", pod.Name, "new-node-id", nodeID).Info("setting node-id label")
		pod.Labels[resources.PodAnnotationNodeIDKey] = realNodeIDStr
		if err := r.Update(ctx, pod, &client.UpdateOptions{}); err != nil {
			return fmt.Errorf(`unable to update pod "%s" with node-id annotation: %w`, pod.Name, err)
		}
	}
	return nil
}

func (r *ClusterReconciler) fetchAdminNodeID(ctx context.Context, rp *vectorizedv1alpha1.Cluster, pod *corev1.Pod, ar *attachedResources) (int32, error) {
	pki, err := ar.getPKI()
	if err != nil {
		return -1, fmt.Errorf("getting pki: %w", err)
	}

	adminClient, err := r.AdminAPIClientFactory(ctx, r.Client, rp, ar.getHeadlessServiceFQDN(), pki.AdminAPIConfigProvider(), pod.Name)
	if err != nil {
		return -1, fmt.Errorf("unable to create admin client: %w", err)
	}
	cfg, err := adminClient.GetNodeConfig(ctx)
	if err != nil {
		return -1, fmt.Errorf("unable to fetch /v1/node_config from %s: %w", pod.Name, err)
	}
	return int32(cfg.NodeID), nil
}

func (r *ClusterReconciler) reportStatus(
	ctx context.Context,
	redpandaCluster *vectorizedv1alpha1.Cluster,
	stSets []*resources.StatefulSetResource,
	internalFQDN string,
	clusterFQDN string,
	schemaRegistryPort int,
	nodeportSvcName types.NamespacedName,
	bootstrapSvcName types.NamespacedName,
) error {
	observedPods, err := r.podList(ctx, redpandaCluster)
	if err != nil {
		return fmt.Errorf("unable to fetch PodList: %w", err)
	}

	observedNodesInternal := make([]string, 0, len(observedPods.Items))
	//nolint:gocritic // the copies are necessary for further redpandacluster updates
	for _, item := range observedPods.Items {
		observedNodesInternal = append(observedNodesInternal, fmt.Sprintf("%s.%s", item.Name, internalFQDN))
	}

	nodeList, err := r.createExternalNodesList(ctx, observedPods.Items, redpandaCluster, nodeportSvcName, bootstrapSvcName)
	if err != nil {
		return fmt.Errorf("failed to construct external node list: %w", err)
	}

	if nodeList == nil {
		nodeList = &vectorizedv1alpha1.NodesList{
			SchemaRegistry: &vectorizedv1alpha1.SchemaRegistryStatus{},
		}
	}
	nodeList.Internal = observedNodesInternal
	nodeList.SchemaRegistry.Internal = fmt.Sprintf("%s:%d", clusterFQDN, schemaRegistryPort)

	if len(stSets) == 0 {
		r.Log.Info("no stateful sets found")
	}

	var version string
	var versionErr error

	nodePoolStatus := make(map[string]vectorizedv1alpha1.NodePoolStatus)
	readyReplicas := int32(0)
	replicas := int32(0)
	currentReplicas := int32(0)
	for _, sts := range stSets {
		if sts.LastObservedState == nil {
			return errNonexistentLastObservedState
		}

		readyReplicas += sts.LastObservedState.Status.ReadyReplicas
		replicas += sts.LastObservedState.Status.Replicas

		oldNps := redpandaCluster.Status.NodePools[sts.GetNodePool().Name]

		oldNps.Replicas = sts.GetReplicas()
		oldNps.ReadyReplicas = sts.LastObservedState.Status.ReadyReplicas

		nodePoolStatus[sts.GetNodePool().Name] = oldNps

		currentReplicas += oldNps.CurrentReplicas

		version, versionErr = sts.CurrentVersion(ctx)
	}

	if versionErr != nil || version == "" {
		r.Log.Info(fmt.Sprintf("cannot get CurrentVersion of statefulset, %s", versionErr))
	}
	if !statusShouldBeUpdated(&redpandaCluster.Status, nodeList, replicas, readyReplicas, version, versionErr, nodePoolStatus) {
		return nil
	}

	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cluster := &vectorizedv1alpha1.Cluster{}
		err := r.Get(ctx, types.NamespacedName{
			Name:      redpandaCluster.Name,
			Namespace: redpandaCluster.Namespace,
		}, cluster)
		if err != nil {
			return err
		}

		cluster.Status.Nodes = *nodeList
		cluster.Status.CurrentReplicas = currentReplicas
		cluster.Status.ReadyReplicas = readyReplicas
		cluster.Status.Replicas = replicas
		if versionErr == nil {
			cluster.Status.Version = version
		}
		cluster.Status.NodePools = nodePoolStatus

		// We may be writing a conflict here.
		err = r.Status().Update(ctx, cluster)
		if err == nil {
			// sync original cluster variable to avoid conflicts on subsequent operations
			*redpandaCluster = *cluster
		}
		return err
	})
	if err != nil {
		return fmt.Errorf("failed to update cluster status: %w", err)
	}
	return nil
}

func statusShouldBeUpdated(
	status *vectorizedv1alpha1.ClusterStatus,
	nodeList *vectorizedv1alpha1.NodesList,
	replicas int32,
	readyReplicas int32,
	newVersion string,
	versionErr error,
	npStatus map[string]vectorizedv1alpha1.NodePoolStatus,
) bool {
	return nodeList != nil &&
		(!reflect.DeepEqual(nodeList.Internal, status.Nodes.Internal) ||
			!reflect.DeepEqual(nodeList.External, status.Nodes.External) ||
			!reflect.DeepEqual(nodeList.ExternalAdmin, status.Nodes.ExternalAdmin) ||
			!reflect.DeepEqual(nodeList.ExternalPandaproxy, status.Nodes.ExternalPandaproxy) ||
			!reflect.DeepEqual(nodeList.SchemaRegistry, status.Nodes.SchemaRegistry) ||
			!reflect.DeepEqual(nodeList.ExternalBootstrap, status.Nodes.ExternalBootstrap)) ||
		!reflect.DeepEqual(npStatus, status.NodePools) ||
		status.Replicas != replicas ||
		status.ReadyReplicas != readyReplicas ||
		(versionErr == nil && status.Version != newVersion)
}

func (r *ClusterReconciler) podList(ctx context.Context, redpandaCluster *vectorizedv1alpha1.Cluster) (corev1.PodList, error) {
	var observedPods corev1.PodList

	err := r.List(ctx, &observedPods, &client.ListOptions{
		LabelSelector: labels.ForCluster(redpandaCluster).AsClientSelector(),
		Namespace:     redpandaCluster.Namespace,
	})
	if err != nil {
		return observedPods, fmt.Errorf("unable to fetch PodList resource: %w", err)
	}

	return observedPods, nil
}

func (r *ClusterReconciler) reconcileClusterForPods(ctx context.Context, pod client.Object) []reconcile.Request {
	return []reconcile.Request{
		{
			NamespacedName: types.NamespacedName{
				Namespace: pod.GetNamespace(),
				Name:      pod.GetLabels()[labels.InstanceKey],
			},
		},
	}
}

func (r *ClusterReconciler) reconcileClusterForExternalCASecret(ctx context.Context, s client.Object) []reconcile.Request {
	hasExternalCA, found := s.GetAnnotations()[SecretAnnotationExternalCAKey]
	if !found || hasExternalCA != "true" { //nolint:goconst //ignored
		return nil
	}

	clusterName, found := s.GetLabels()[labels.InstanceKey]
	if !found {
		return nil
	}

	return []reconcile.Request{{
		NamespacedName: types.NamespacedName{
			Namespace: s.GetNamespace(),
			Name:      clusterName,
		},
	}}
}

// WithConfiguratorSettings set the configurator image settings
func (r *ClusterReconciler) WithConfiguratorSettings(
	configuratorSettings resources.ConfiguratorSettings,
) *ClusterReconciler {
	r.configuratorSettings = configuratorSettings
	return r
}

// WithClusterDomain set the clusterDomain
func (r *ClusterReconciler) WithClusterDomain(
	clusterDomain string,
) *ClusterReconciler {
	r.clusterDomain = clusterDomain
	return r
}

//nolint:funlen,gocyclo // External nodes list should be refactored
func (r *ClusterReconciler) createExternalNodesList(
	ctx context.Context,
	pods []corev1.Pod,
	pandaCluster *vectorizedv1alpha1.Cluster,
	nodePortName types.NamespacedName,
	bootstrapName types.NamespacedName,
) (*vectorizedv1alpha1.NodesList, error) {
	externalKafkaListener := pandaCluster.ExternalListener()
	externalAdminListener := pandaCluster.AdminAPIExternal()
	externalProxyListener := pandaCluster.PandaproxyAPIExternal()
	schemaRegistryConf := pandaCluster.Spec.Configuration.SchemaRegistry
	if externalKafkaListener == nil && externalAdminListener == nil && externalProxyListener == nil &&
		(schemaRegistryConf == nil || !pandaCluster.IsSchemaRegistryExternallyAvailable()) {
		return nil, nil
	}

	var nodePortSvc corev1.Service
	if err := r.Get(ctx, nodePortName, &nodePortSvc); err != nil {
		return nil, fmt.Errorf("failed to retrieve node port service %s: %w", nodePortName, err)
	}

	for _, port := range nodePortSvc.Spec.Ports {
		if port.NodePort == 0 {
			return nil, fmt.Errorf("node port service %s, port %s is 0: %w", nodePortName, port.Name, errNodePortMissing)
		}
	}

	var node corev1.Node
	result := &vectorizedv1alpha1.NodesList{
		External:           make([]string, 0, len(pods)),
		ExternalAdmin:      make([]string, 0, len(pods)),
		ExternalPandaproxy: make([]string, 0, len(pods)),
		SchemaRegistry: &vectorizedv1alpha1.SchemaRegistryStatus{
			Internal:        "",
			External:        "",
			ExternalNodeIPs: make([]string, 0, len(pods)),
		},
	}

	for i := range pods {
		pod := pods[i]

		if externalKafkaListener != nil && needExternalIP(externalKafkaListener.External) ||
			externalAdminListener != nil && needExternalIP(externalAdminListener.External) ||
			externalProxyListener != nil && needExternalIP(externalProxyListener.External.ExternalConnectivityConfig) ||
			schemaRegistryConf != nil && schemaRegistryConf.External != nil && needExternalIP(*schemaRegistryConf.GetExternal()) {
			if err := r.Get(ctx, types.NamespacedName{Name: pods[i].Spec.NodeName}, &node); err != nil {
				return nil, fmt.Errorf("failed to retrieve node %s: %w", pods[i].Spec.NodeName, err)
			}
		}

		if externalKafkaListener != nil && externalKafkaListener.External.Subdomain != "" {
			address, err := subdomainAddress(externalKafkaListener.External.EndpointTemplate, &pod, externalKafkaListener.External.Subdomain, getNodePort(&nodePortSvc, resources.ExternalListenerName), pandaCluster)
			if err != nil {
				return nil, err
			}
			result.External = append(result.External, address)
		} else if externalKafkaListener != nil {
			result.External = append(result.External,
				fmt.Sprintf("%s:%d",
					networking.GetPreferredAddress(&node, corev1.NodeAddressType(externalKafkaListener.External.PreferredAddressType)),
					getNodePort(&nodePortSvc, resources.ExternalListenerName),
				))
		}

		if externalAdminListener != nil && externalAdminListener.External.Subdomain != "" {
			address, err := subdomainAddress(externalAdminListener.External.EndpointTemplate, &pod, externalAdminListener.External.Subdomain, getNodePort(&nodePortSvc, resources.AdminPortExternalName), pandaCluster)
			if err != nil {
				return nil, err
			}
			result.ExternalAdmin = append(result.ExternalAdmin, address)
		} else if externalAdminListener != nil {
			result.ExternalAdmin = append(result.ExternalAdmin,
				fmt.Sprintf("%s:%d",
					getExternalIP(&node),
					getNodePort(&nodePortSvc, resources.AdminPortExternalName),
				))
		}

		if externalProxyListener != nil && externalProxyListener.External.Subdomain != "" {
			address, err := subdomainAddress(externalProxyListener.External.EndpointTemplate, &pod, externalProxyListener.External.Subdomain, getNodePort(&nodePortSvc, resources.PandaproxyPortExternalName), pandaCluster)
			if err != nil {
				return nil, err
			}
			result.ExternalPandaproxy = append(result.ExternalPandaproxy, address)
		} else if externalProxyListener != nil {
			result.ExternalPandaproxy = append(result.ExternalPandaproxy,
				fmt.Sprintf("%s:%d",
					getExternalIP(&node),
					getNodePort(&nodePortSvc, resources.PandaproxyPortExternalName),
				))
		}

		if schemaRegistryConf != nil && schemaRegistryConf.External != nil && needExternalIP(*schemaRegistryConf.GetExternal()) {
			result.SchemaRegistry.ExternalNodeIPs = append(result.SchemaRegistry.ExternalNodeIPs,
				fmt.Sprintf("%s:%d",
					getExternalIP(&node),
					getNodePort(&nodePortSvc, resources.SchemaRegistryPortName),
				))
		}
	}

	if schemaRegistryConf != nil && schemaRegistryConf.External != nil && schemaRegistryConf.External.Subdomain != "" {
		prefix := ""
		if schemaRegistryConf.External.Endpoint != "" {
			prefix = fmt.Sprintf("%s.", schemaRegistryConf.External.Endpoint)
		}
		result.SchemaRegistry.External = fmt.Sprintf("%s%s:%d",
			prefix,
			schemaRegistryConf.External.Subdomain,
			getNodePort(&nodePortSvc, resources.SchemaRegistryPortName),
		)
	}

	if externalProxyListener != nil && externalProxyListener.External.Subdomain != "" {
		result.PandaproxyIngress = &externalProxyListener.External.Subdomain
	}

	if externalKafkaListener.External.Bootstrap != nil {
		var bootstrapSvc corev1.Service
		if err := r.Get(ctx, bootstrapName, &bootstrapSvc); err != nil {
			return nil, fmt.Errorf("failed to retrieve bootstrap lb service %s: %w", bootstrapName, err)
		}
		result.ExternalBootstrap = &vectorizedv1alpha1.LoadBalancerStatus{
			LoadBalancerStatus: bootstrapSvc.Status.LoadBalancer,
		}
	}
	return result, nil
}

func (r *ClusterReconciler) removeFinalizers(
	ctx context.Context, redpandaCluster *vectorizedv1alpha1.Cluster, l logr.Logger,
) error {
	log := l.WithName("removeFinalizers")
	log.V(logger.DebugLevel).Info("handling finalizer removal")

	if controllerutil.ContainsFinalizer(redpandaCluster, FinalizerKey) {
		log.V(logger.DebugLevel).Info("removing finalizers from cluster custom resource")
		p := client.MergeFrom(redpandaCluster.DeepCopy())
		controllerutil.RemoveFinalizer(redpandaCluster, FinalizerKey)
		if err := r.Patch(ctx, redpandaCluster, p); err != nil {
			return fmt.Errorf("unable to remove Cluster finalizer: %w", err)
		}
	}

	pods, err := r.podList(ctx, redpandaCluster)
	if err != nil {
		return fmt.Errorf("unable to list Pods: %w", err)
	}

	for i := range pods.Items {
		if err := r.removePodFinalizer(ctx, &pods.Items[i], log); err != nil {
			return fmt.Errorf(`unable to remove finalizer for pod "%s": %w`, pods.Items[i].GetName(), err)
		}
	}
	return nil
}

// setInitialSuperUserPassword should be idempotent, create user if not found, updates if found
func (r *ClusterReconciler) setInitialSuperUserPassword(
	ctx context.Context,
	adminAPI adminutils.AdminAPIClient,
	objs []types.NamespacedName,
) error {
	// might not have internal AdminAPI listener
	if adminAPI == nil {
		return nil
	}

	// list out users here
	users, err := adminAPI.ListUsers(ctx)
	if err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "unavailable") {
			return fmt.Errorf("could not fetch users from the Redpanda admin api: %w", err)
		}
	}

	errs := make([]error, 0)
	for _, obj := range objs {
		// We first check that the secret has been created
		// This should have been done by this point, if not
		// requeue, this is created by the controller and is
		// the source of truth, not the admin API
		secret := &corev1.Secret{}
		errGet := r.Get(ctx, obj, secret)
		if errGet != nil {
			errs = append(errs, fmt.Errorf("could not fetch user secret (%s): %w", obj.String(), errGet))
			continue
		}
		// if we do not find the user in the list, we should create them
		userFound := false
		expectedUser := string(secret.Data[corev1.BasicAuthUsernameKey])

		for i := range users {
			if users[i] == expectedUser {
				userFound = true
				break
			}
		}

		if userFound {
			// update the user here, we cannot retrieve password changes here to date
			if updateErr := updateUserOnAdminAPI(ctx, adminAPI, secret); updateErr != nil {
				errs = append(errs, fmt.Errorf("redpanda admin api: %w", updateErr))
			}
			continue
		}

		// we did not find user, so create them
		if createErr := createUserOnAdminAPI(ctx, adminAPI, secret); createErr != nil {
			errs = append(errs, fmt.Errorf("redpanda admin api: %w", createErr))
		}
	}

	return utilerrors.NewAggregate(errs)
}

// custom error to satisfy err113
type missingBrokerIDError struct{}

func (m *missingBrokerIDError) Error() string {
	return "a pod is temporarily missing the broker-id annotation"
}

// rpBrokerList returns Redpanda view of registered brokers. Health overview of the cluster is just
// an information during debugging.
func (r *ClusterReconciler) rpBrokerList(ctx context.Context, vCluster *vectorizedv1alpha1.Cluster, ar *attachedResources) (adminutils.AdminAPIClient, []rpadmin.Broker, *rpadmin.ClusterHealthOverview, error) {
	pki, err := ar.getPKI()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("getting pki: %w", err)
	}

	adminClient, err := r.AdminAPIClientFactory(ctx, r.Client, vCluster, ar.getHeadlessServiceFQDN(), pki.AdminAPIConfigProvider())
	if err != nil {
		return nil, nil, nil, fmt.Errorf("creating admin client: %w", err)
	}

	bl, err := adminClient.Brokers(ctx)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("retrieving broker list: %w", err)
	}

	// Regardless of the problem with getting cluster health overview return broker list
	health, _ := adminClient.GetHealthOverview(ctx)

	return adminClient, bl, &health, nil
}

// decommissionGhostBrokers will calculate the difference between Broker IDs reported
// in Pod Annotations map and the broker list returned from Redpanda Admin API. Any
// Redpanda reported as not alive would be decommissioned.
// This is not a reversible process. If creating a new broker due to an empty disk was a mistake, the data
// that was on that disk will be unusable.
// This function will not requeue Cluster reconciliation loop.
func (r *ClusterReconciler) decommissionGhostBrokers(c context.Context, vCluster *vectorizedv1alpha1.Cluster, l logr.Logger, ar *attachedResources) {
	ctx, done := context.WithCancel(c)
	defer done()
	log := l.WithName("doDecommissionGhostBrokers")
	log.Info("deleting ghost brokers")

	adminClient, bl, health, err := r.rpBrokerList(ctx, vCluster, ar)
	if err != nil {
		log.Error(err, "stopping decommission verification due to missing broker list")
		return
	}

	pods, err := r.podList(ctx, vCluster)
	if err != nil {
		log.Error(err, "unable to fetch PodList")
		return
	}

	// Only consider pods that are running and healthy
	pods.Items = slices.DeleteFunc(pods.Items, func(p corev1.Pod) bool {
		return p.Status.Phase != corev1.PodRunning
	})

	if int32(len(pods.Items)) != vCluster.Status.Replicas {
		log.Info("skipping ghost broker detection, because not all pods are ready.",
			"replica-number", vCluster.Status.Replicas,
			"pod-len", len(pods.Items))
		return
	}

	// Create map of existing Redpanda IDs from Redpanda Pod Annotations.
	actualBrokerIDs := make(map[int]any, len(pods.Items))
	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.Annotations == nil {
			log.Error(&missingBrokerIDError{}, "missing annotations in pod", "pod-name", pod.Name)
			return
		}

		nodeIDStrAnnotation, annotationExist := pod.Annotations[resources.PodAnnotationNodeIDKey]
		if !annotationExist {
			log.Error(&missingBrokerIDError{}, "annotations does not have broker id annotation", "pod-name", pod.Name)
			return
		}

		var id int
		id, err = strconv.Atoi(nodeIDStrAnnotation)
		if err != nil {
			log.Error(fmt.Errorf("pod %s has an invalid broker-id annotation: %q: %w", pod.Name, nodeIDStrAnnotation, err),
				"skipping ghost broker checks due to invalid broker ID in annotation",
				"current-broker-ids-map", actualBrokerIDs,
				"pod-name", pod.Name, "pod-annotation", nodeIDStrAnnotation)
			return
		}

		actualBrokerIDs[id] = nil
	}

	var nodesConsideredDown []int
	for _, b := range bl {
		_, isInK8S := actualBrokerIDs[b.NodeID]
		if !isInK8S && b.MembershipStatus == rpadmin.MembershipStatusActive && b.IsAlive != nil && !*b.IsAlive {
			nodesConsideredDown = append(nodesConsideredDown, b.NodeID)
		}
	}

	log.Info("Nodes that are reported by Redpanda, but are not running in Kubernetes cluster",
		"nodes-considered-down", nodesConsideredDown,
		"cluster-health", health,
		"broker-list", bl,
		"actual-broker-ids", actualBrokerIDs)
	for _, ndID := range nodesConsideredDown {
		l.Info("decommissioning ghost broker", "node-id", ndID)
		if err = adminClient.DecommissionBroker(ctx, ndID); err != nil {
			log.Error(err, "failed to decommission ghost broker",
				"node-id", ndID,
				"nodes-considered-down", nodesConsideredDown,
				"cluster-health", health,
				"broker-list", bl,
				"actual-broker-ids", actualBrokerIDs)
			continue
		}
	}
}

// createUserOnAdminAPI will return to requeue only when api error occurred
func createUserOnAdminAPI(ctx context.Context, adminAPI adminutils.AdminAPIClient, secret *corev1.Secret) error {
	username := string(secret.Data[corev1.BasicAuthUsernameKey])
	password := string(secret.Data[corev1.BasicAuthPasswordKey])

	err := adminAPI.CreateUser(ctx, username, password, rpadmin.ScramSha256)
	// {"message": "Creating user: User already exists", "code": 400}
	if err != nil { // TODO if user already exists, we only receive "400". Check for specific error code when available.
		if strings.Contains(err.Error(), "already exists") {
			return nil
		}
		return fmt.Errorf("could not create user %q: %w", username, err)
	}
	return err
}

func updateUserOnAdminAPI(ctx context.Context, adminAPI adminutils.AdminAPIClient, secret *corev1.Secret) error {
	username := string(secret.Data[corev1.BasicAuthUsernameKey])
	password := string(secret.Data[corev1.BasicAuthPasswordKey])

	err := adminAPI.UpdateUser(ctx, username, password, rpadmin.ScramSha256)
	if err != nil {
		return fmt.Errorf("could not update user %q: %w", username, err)
	}

	return err
}

func needExternalIP(external vectorizedv1alpha1.ExternalConnectivityConfig) bool {
	return external.Subdomain == ""
}

func subdomainAddress(
	tmpl string, pod *corev1.Pod, subdomain string, port int32, pandaCluster *vectorizedv1alpha1.Cluster,
) (string, error) {
	prefixLen := len(pod.GenerateName)
	index, err := strconv.Atoi(pod.Name[prefixLen:])
	if err != nil {
		return "", fmt.Errorf("could not parse node ID from pod name %s: %w", pod.Name, err)
	}
	var hostIndexOffset int
	if val, ok := pod.GetAnnotations()[labels.NodePoolKey]; ok {
		for _, np := range pandaCluster.GetNodePoolsFromSpec() {
			if np.Name == val {
				hostIndexOffset = np.HostIndexOffset
				break
			}
		}
	}
	data := utils.NewEndpointTemplateData(index, pod.Status.HostIP, hostIndexOffset)
	ep, err := utils.ComputeEndpoint(tmpl, data)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s.%s:%d",
		ep,
		subdomain,
		port,
	), nil
}

func getExternalIP(node *corev1.Node) string {
	if node == nil {
		return ""
	}
	for _, address := range node.Status.Addresses {
		if address.Type == corev1.NodeExternalIP {
			return address.Address
		}
	}
	return ""
}

func getNodePort(svc *corev1.Service, name string) int32 {
	if svc == nil {
		return -1
	}
	for _, port := range svc.Spec.Ports {
		if port.Name == name && port.NodePort != 0 {
			return port.NodePort
		}
	}
	return 0
}

func collectNodePorts(
	redpandaPorts *networking.RedpandaPorts,
) []resources.NamedServiceNodePort {
	nodeports := []resources.NamedServiceNodePort{}
	kafkaAPINamedNodePort := redpandaPorts.KafkaAPI.ToNamedServiceNodePort()
	if kafkaAPINamedNodePort != nil {
		nodeports = append(nodeports, *kafkaAPINamedNodePort)
	}
	adminAPINodePort := redpandaPorts.AdminAPI.ToNamedServiceNodePort()
	if adminAPINodePort != nil {
		nodeports = append(nodeports, *adminAPINodePort)
	}
	pandaProxyNodePort := redpandaPorts.PandaProxy.ToNamedServiceNodePort()
	if pandaProxyNodePort != nil {
		nodeports = append(nodeports, *pandaProxyNodePort)
	}
	schemaRegistryNodePort := redpandaPorts.SchemaRegistry.ToNamedServiceNodePort()
	if schemaRegistryNodePort != nil {
		nodeports = append(nodeports, *schemaRegistryNodePort)
	}
	return nodeports
}

func collectHeadlessPorts(
	redpandaPorts *networking.RedpandaPorts,
) []resources.NamedServicePort {
	headlessPorts := []resources.NamedServicePort{}
	if redpandaPorts.AdminAPI.Internal != nil {
		headlessPorts = append(headlessPorts, resources.NamedServicePort{Name: resources.AdminPortName, Port: *redpandaPorts.AdminAPI.InternalPort()})
	}
	if redpandaPorts.KafkaAPI.Internal != nil {
		headlessPorts = append(headlessPorts, resources.NamedServicePort{Name: resources.InternalListenerName, Port: *redpandaPorts.KafkaAPI.InternalPort()})
	}
	if redpandaPorts.PandaProxy.Internal != nil {
		headlessPorts = append(headlessPorts, resources.NamedServicePort{Name: resources.PandaproxyPortInternalName, Port: *redpandaPorts.PandaProxy.InternalPort()})
	}
	return headlessPorts
}

func collectLBPorts(
	redpandaPorts *networking.RedpandaPorts,
) []resources.NamedServicePort {
	lbPorts := []resources.NamedServicePort{}
	if redpandaPorts.KafkaAPI.ExternalBootstrap != nil {
		lbPorts = append(lbPorts, *redpandaPorts.KafkaAPI.ExternalBootstrap)
	}
	return lbPorts
}

func collectClusterPorts(
	redpandaPorts *networking.RedpandaPorts,
	redpandaCluster *vectorizedv1alpha1.Cluster,
) []resources.NamedServicePort {
	clusterPorts := []resources.NamedServicePort{}
	if redpandaPorts.PandaProxy.External != nil {
		clusterPorts = append(clusterPorts, resources.NamedServicePort{Name: resources.PandaproxyPortExternalName, Port: *redpandaPorts.PandaProxy.ExternalPort()})
	}
	if redpandaCluster.Spec.Configuration.SchemaRegistry != nil {
		port := redpandaCluster.Spec.Configuration.SchemaRegistry.Port
		clusterPorts = append(clusterPorts, resources.NamedServicePort{Name: resources.SchemaRegistryPortName, Port: port})
	}
	if redpandaPorts.KafkaAPI.Internal != nil {
		port := redpandaPorts.KafkaAPI.Internal.Port
		clusterPorts = append(clusterPorts, resources.NamedServicePort{Name: resources.InternalListenerName, Port: port})
	}
	if redpandaPorts.AdminAPI.Internal != nil {
		port := redpandaPorts.AdminAPI.Internal.Port
		clusterPorts = append(clusterPorts, resources.NamedServicePort{Name: resources.AdminPortName, Port: port})
	}

	return clusterPorts
}

func isRedpandaClusterManaged(
	l logr.Logger, redpandaCluster *vectorizedv1alpha1.Cluster,
) bool {
	log := l.WithName("isRedpandaClusterManaged")
	managedAnnotationKey := vectorizedv1alpha1.GroupVersion.Group + managedPath
	if managed, exists := redpandaCluster.Annotations[managedAnnotationKey]; exists && managed == NotManaged {
		log.Info(fmt.Sprintf("management is disabled; to enable it, change the '%s' annotation to true or remove it",
			managedAnnotationKey))
		return false
	}
	return true
}

func isRedpandaClusterVersionManaged(
	l logr.Logger,
	redpandaCluster *vectorizedv1alpha1.Cluster,
	restrictToRedpandaVersion string,
) bool {
	log := l.WithName("isRedpandaClusterVersionManaged").WithValues("restrictToRedpandaVersion", restrictToRedpandaVersion, "cluster spec.version", redpandaCluster.Status.Version)
	if restrictToRedpandaVersion != "" && restrictToRedpandaVersion != redpandaCluster.Spec.Version {
		log.Info("not managed due to version management restriction")
		return false
	}
	return true
}

func getQuiescentCondition(redpandaCluster *vectorizedv1alpha1.Cluster) vectorizedv1alpha1.ClusterCondition {
	condition := vectorizedv1alpha1.ClusterCondition{
		Type: vectorizedv1alpha1.OperatorQuiescentConditionType,
	}

	for npName, np := range redpandaCluster.Status.NodePools {

		idx := slices.IndexFunc(redpandaCluster.Spec.NodePools, func(npSpec vectorizedv1alpha1.NodePoolSpec) bool {
			return npSpec.Name == npName
		})
		if idx != -1 {
			npSpec := redpandaCluster.Spec.NodePools[idx]
			if npSpec.Replicas != nil && np.Replicas != *npSpec.Replicas {
				condition.Status = corev1.ConditionFalse
				condition.Reason = "NodePoolNotSynced"
				condition.Message = fmt.Sprintf("NodePool %s replicas are not synced. spec.replicas=%d,status.replicas=%d. They must be equal.", npName, *npSpec.Replicas, np.Replicas)
				return condition

			}
		}

		if np.CurrentReplicas != np.Replicas || np.CurrentReplicas != np.ReadyReplicas {
			condition.Status = corev1.ConditionFalse
			condition.Reason = "NodePoolNotSynced"
			condition.Message = fmt.Sprintf("NodePool %s replicas are not synced. currentReplicas=%d,replicas=%d,readyReplicas=%d. All must be equal.", npName, np.CurrentReplicas, np.Replicas, np.ReadyReplicas)
			return condition
		}
	}

	if redpandaCluster.Status.DecommissioningNode != nil {
		condition.Status = corev1.ConditionFalse
		condition.Reason = "DecommissioningInProgress"
		condition.Message = fmt.Sprintf("Decommissioning of node_id=%d in progress", *redpandaCluster.Status.DecommissioningNode)
		return condition
	}

	if redpandaCluster.Status.Restarting {
		condition.Status = corev1.ConditionFalse
		condition.Reason = "Restarting"
		condition.Message = "Cluster is restarting"
		return condition
	}

	if redpandaCluster.Spec.Version != redpandaCluster.Status.Version && redpandaCluster.Status.Version != "" {
		condition.Status = corev1.ConditionFalse
		condition.Reason = "UpgradeInProgress"
		condition.Message = fmt.Sprintf("Upgrade from %s to %s in progress", redpandaCluster.Spec.Version, redpandaCluster.Status.Version)
		return condition
	}

	// No reason found (no early return), so claim the controller is quiescent.
	condition.Status = corev1.ConditionTrue
	return condition
}
