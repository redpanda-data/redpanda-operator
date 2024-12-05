// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package decommissioning

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/redpanda-data/common-go/rpadmin"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/kubernetes"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/collections"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/functional"
)

const (
	eventReasonBroker                        = "DecommissioningBroker"
	eventReasonUnboundPersistentVolumeClaims = "DecommissioningUnboundPersistentVolumeClaims"

	k8sManagedByLabelKey = "app.kubernetes.io/managed-by"
	k8sInstanceLabelKey  = "app.kubernetes.io/instance"
	k8sComponentLabelKey = "app.kubernetes.io/component"
	k8sNameLabelKey      = "app.kubernetes.io/name"
	datadirVolume        = "datadir"

	traceLevel = 2
	debugLevel = 1
	infoLevel  = 0

	defaultRequeueTimeout = 10 * time.Second
)

type Option func(*StatefulSetDecomissioner)

func FilterStatefulSetOwner(ownerNamespace, ownerName string) func(ctx context.Context, set *appsv1.StatefulSet) (bool, error) {
	filter := filterOwner(ownerNamespace, ownerName)
	return func(ctx context.Context, set *appsv1.StatefulSet) (bool, error) {
		return filter(set), nil
	}
}

func filterOwner(ownerNamespace, ownerName string) func(o client.Object) bool {
	return func(o client.Object) bool {
		labels := o.GetLabels()
		if o.GetNamespace() == ownerNamespace && labels != nil && labels[k8sInstanceLabelKey] == ownerName {
			return true
		}
		return false
	}
}

func WithFilter(filter func(ctx context.Context, set *appsv1.StatefulSet) (bool, error)) Option {
	return func(decommissioner *StatefulSetDecomissioner) {
		decommissioner.filter = filter
	}
}

func WithFactory(factory internalclient.ClientFactory) Option {
	return func(decommissioner *StatefulSetDecomissioner) {
		decommissioner.factory = factory
	}
}

func WithRequeueTimeout(timeout time.Duration) Option {
	return func(decommissioner *StatefulSetDecomissioner) {
		decommissioner.requeueTimeout = timeout
	}
}

type StatefulSetDecomissioner struct {
	client         client.Client
	factory        internalclient.ClientFactory
	fetcher        ValuesFetcher
	recorder       record.EventRecorder
	requeueTimeout time.Duration
	filter         func(ctx context.Context, set *appsv1.StatefulSet) (bool, error)
}

func NewStatefulSetDecommissioner(mgr ctrl.Manager, fetcher ValuesFetcher, options ...Option) *StatefulSetDecomissioner {
	k8sClient := mgr.GetClient()

	decommissioner := &StatefulSetDecomissioner{
		recorder:       mgr.GetEventRecorderFor("broker-decommissioner"),
		client:         k8sClient,
		fetcher:        fetcher,
		factory:        internalclient.NewFactory(mgr.GetConfig(), k8sClient),
		requeueTimeout: defaultRequeueTimeout,
		filter:         func(ctx context.Context, set *appsv1.StatefulSet) (bool, error) { return true, nil },
	}

	for _, opt := range options {
		opt(decommissioner)
	}

	return decommissioner
}

// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=get;list;watch;delete
// +kubebuilder:rbac:groups=core,resources=persistentvolumes,verbs=patch
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch

// +kubebuilder:rbac:groups=apps,namespace=default,resources=statefulsets,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,namespace=default,resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,namespace=default,resources=persistentvolumeclaims,verbs=get;list;watch;delete
// +kubebuilder:rbac:groups=core,namespace=default,resources=persistentvolumes,verbs=patch
// +kubebuilder:rbac:groups=core,namespace=default,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=core,namespace=default,resources=secrets,verbs=get;list;watch

func (s *StatefulSetDecomissioner) Setup(mgr ctrl.Manager) error {
	pvcPredicate, err := predicate.LabelSelectorPredicate(
		metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{{
				Key:      k8sNameLabelKey, // look for only redpanda owned pvcs
				Operator: metav1.LabelSelectorOpIn,
				Values:   []string{"redpanda"},
			}, {
				Key:      k8sComponentLabelKey, // make sure the PVC is part of the statefulset
				Operator: metav1.LabelSelectorOpIn,
				Values:   []string{"redpanda-statefulset"},
			}, {
				Key:      k8sInstanceLabelKey, // make sure we have a cluster name
				Operator: metav1.LabelSelectorOpExists,
			}},
		},
	)
	if err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1.StatefulSet{}).
		Owns(&corev1.Pod{}).
		// PVCs don't have a "true" owner ref, so instead we attempt to map backwards via labels
		Watches(&corev1.PersistentVolumeClaim{}, handler.EnqueueRequestsFromMapFunc(func(_ context.Context, o client.Object) []ctrl.Request {
			claim := o.(*corev1.PersistentVolumeClaim)
			labels := claim.GetLabels()

			// a bit of defensive programming, but we should always have labels due to our use
			// of a predicate
			if labels == nil {
				// we have no labels, so we can't map anything
				return nil
			}

			release := labels[k8sInstanceLabelKey]
			if release == "" {
				// we have an invalid release name, so skip
				return nil
			}

			if !strings.HasPrefix(claim.Name, datadirVolume+"-") {
				// we only care about the datadir volume
				return nil
			}

			// if we are here, it means we can map to a real stateful set
			return []ctrl.Request{
				{NamespacedName: types.NamespacedName{
					Name:      release,
					Namespace: claim.Namespace,
				}},
			}
		}), builder.WithPredicates(pvcPredicate)).
		Complete(s)
}

func (s *StatefulSetDecomissioner) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx, "namespace", req.Namespace, "name", req.Name).WithName("StatefulSetDecomissioner.Reconcile")

	set := &appsv1.StatefulSet{}
	if err := s.client.Get(ctx, req.NamespacedName, set); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, "fetching StatefulSet")

		// avoid the internal controller runtime stacktrace
		return ctrl.Result{Requeue: true}, nil
	}

	// skip objects already being deleted
	if !set.ObjectMeta.DeletionTimestamp.IsZero() {
		// TODO: This follows the original implementation, but
		// won't this leave orphaned PVCs around?
		log.V(traceLevel).Info("StatefulSet is currently deleted, skipping")

		return ctrl.Result{}, nil
	}

	requeue, err := s.Decommission(ctx, set)
	if err != nil {
		// we already logged any error, just requeue directly
		return ctrl.Result{Requeue: true}, nil
	}

	if requeue {
		return ctrl.Result{RequeueAfter: s.requeueTimeout}, nil
	}

	return ctrl.Result{}, nil
}

// Decommission decommissions any stray resources for a StatefulSet. This includes:
//
// 1. Orphaned PVCs created by the StatefulSet
// 2. Any old brokers that exist in Redpanda and need to be explicitly decommissioned via the admin API
//
// It has the following rough flow:
//
//  1. Filter and manage only particular StatefulSets via a specified user filter (for running as a sidecar)
//  2. Find associated unbound PVCs
//  3. If an unbound PVC exists, delete it after attempting to set its backing PV to have a retain policy
//  4. Get the health status of our cluster and compare the existing number of nodes with the desired amount
//  5. If we have more nodes than desired, attempt to decommission the downed nodes via checking:
//     a. That the broker's ordinal parsed from its internal advertised address exceeds the max ordinal
//     that the stateful set would produce
//     b. If it doesn't, check to see if other node ordinals collide with this node ordinal
//     c. If they do and one of them is healthy, then any unhealthy nodes with the same ordinal
//     can be decommissioned
//  6. If any broker is currently being decommissioned, wait until that process is complete
//  7. Once it is, decommission the brokers starting with the broker with the lowest node id
//
// For PVC deletion and broker decommissioning, each step happens sequentially such that no two brokers should
// attempt to be decommissioned simultaneously. Likewise each PVC is deleted one by one.
func (s *StatefulSetDecomissioner) Decommission(ctx context.Context, set *appsv1.StatefulSet) (bool, error) {
	// note that this is best-effort, the decommissioning code needs to be idempotent and deterministic

	log := ctrl.LoggerFrom(ctx, "namespace", set.Namespace, "name", set.Name).WithName("StatefulSetDecommissioner.Decomission")

	// if helm is not managing it, move on.
	if managedBy, ok := set.Labels[k8sManagedByLabelKey]; managedBy != "Helm" || !ok {
		log.V(traceLevel).Info("not managed by helm")
		return false, nil
	}

	keep, err := s.filter(ctx, set)
	if err != nil {
		log.Error(err, "error filtering StatefulSet")
		return false, err
	}

	if !keep {
		log.V(traceLevel).Info("skipping decommission, StatefulSet filtered out")
		return false, nil
	}

	unboundVolumeClaims, err := s.findUnboundVolumeClaims(ctx, set)
	if err != nil {
		log.Error(err, "error finding unbound PersistentVolumeClaims")
		return false, err
	}

	log.V(traceLevel).Info("fetched unbound volume claims", "claims", functional.MapFn(func(claim *corev1.PersistentVolumeClaim) string {
		return claim.Name
	}, unboundVolumeClaims))

	// we first clean up any unbound PVCs, ensuring that their PVs have a retain policy
	if len(unboundVolumeClaims) > 0 {
		claim := unboundVolumeClaims[0]
		volume := &corev1.PersistentVolume{
			ObjectMeta: metav1.ObjectMeta{
				Name: claim.Spec.VolumeName,
			},
		}

		// ensure that the PV has a retain policy
		if err := s.client.Patch(ctx, volume, kubernetes.ApplyPatch(corev1ac.PersistentVolume(volume.Name).WithSpec(
			corev1ac.PersistentVolumeSpec().WithPersistentVolumeReclaimPolicy(corev1.PersistentVolumeReclaimRetain),
		)), client.ForceOwnership, client.FieldOwner("owner")); err != nil {
			log.Error(err, "error patching PersistentVolume spec")
			return false, err
		}

		// now that we've patched the PV, delete the PVC
		if err := s.client.Delete(ctx, claim); err != nil {
			log.Error(err, "error deleting PersistentVolumeClaim")
			return false, err
		}

		message := fmt.Sprintf(
			"unbound persistent volume claims: [%s], decommissioning: %s", strings.Join(functional.MapFn(func(claim *corev1.PersistentVolumeClaim) string {
				return client.ObjectKeyFromObject(claim).String()
			}, unboundVolumeClaims), ", "), client.ObjectKeyFromObject(claim).String(),
		)

		log.V(traceLevel).Info(message)
		s.recorder.Eventf(set, corev1.EventTypeNormal, eventReasonUnboundPersistentVolumeClaims, message)

		// at this point we should get a requeue anyway due to the ownership watch
		// so just delegate to the runtime
		return false, nil
	}

	// now we check if we can/should decommission any brokers
	adminClient, err := s.getAdminClient(ctx, set)
	if err != nil {
		log.Error(err, "initializing admin client")
		return false, err
	}

	health, err := adminClient.GetHealthOverview(ctx)
	if err != nil {
		log.Error(err, "fetching brokers")
		return false, err
	}

	requestedNodes := int(ptr.Deref(set.Spec.Replicas, 0))
	if len(health.AllNodes) <= requestedNodes {
		// we don't need to decommission anything since we're at the proper
		// capacity
		return false, nil
	}

	if len(health.NodesDown) == 0 {
		// we don't need to decommission anything since everything is healthy
		// and we want to wait until a broker is fully stopped
		return false, nil
	}

	allNodes := collections.NewSet[int]()
	allNodes.Add(health.AllNodes...)
	downedNodes := collections.NewSet[int]()
	downedNodes.Add(health.NodesDown...)
	healthyNodes := allNodes.LeftDisjoint(downedNodes)

	brokerOrdinalMap := map[int]collections.Set[int]{}
	brokerMap := map[int]int{}
	for _, brokerID := range health.AllNodes {
		broker, err := adminClient.Broker(ctx, brokerID)
		if err != nil {
			log.Error(err, "fetching broker")
			return false, err
		}

		// NB: We capture the ordinal here because it gives us the
		// ability to sort and have an extra check to ensure that we're only decommissioning
		// downed brokers who also have ordinals that are higher than what the stateful set
		// in its current configuration would actually produce (i.e. we
		// don't want to accidentally decommission any random unhealthy brokers).
		//
		// Additionally, any potential ordinal collisions tell us that maybe the node
		// id has changed but the original pod is actually gone (i.e. like a ghost broker)
		// when we have an ordinal collision, then we check to make sure we have at least one
		// healthy node for the given ordinal before decommissioning.
		ordinal, err := ordinalFromFQDN(broker.InternalRPCAddress)
		if err != nil {
			// continue since we can't tell whether we can decommission this or not
			// but make a lot of noise about the fact that we can't map this back to
			// an ordinal
			log.Error(err, "unexpected error parsing broker pod ordinal", "address", broker.InternalRPCAddress, "broker", broker)
			continue
		}

		if _, ok := brokerOrdinalMap[ordinal]; !ok {
			brokerOrdinalMap[ordinal] = collections.NewSet[int]()
		}

		// NB: here we have potentially multiple brokers that align to the same internal RPC address
		// if that's the case, then one of them is going to be bad and can be decommissioned
		brokerOrdinalMap[ordinal].Add(brokerID)
		brokerMap[brokerID] = ordinal
	}

	brokersToDecommission := []int{}
	brokersToIgnore := []int{}
	currentlyDecommissioningBrokers := []int{}

	for _, downedNode := range health.NodesDown {
		ordinal, ok := brokerMap[downedNode]
		if !ok {
			// skip because we can't actually determine whether we should
			// decommission it or not without its ordinal
			brokersToIgnore = append(brokersToIgnore, downedNode)
		}

		status, err := adminClient.DecommissionBrokerStatus(ctx, downedNode)
		if err != nil {
			if strings.Contains(err.Error(), "is not decommissioning") {
				if ordinal >= requestedNodes {
					// this broker is old and should be deleted
					brokersToDecommission = append(brokersToDecommission, downedNode)
					continue
				}

				brokers := brokerOrdinalMap[ordinal]
				if brokers.Size() == 1 {
					// just ignore the node since it may be down, but it probably
					// is just having problems
					brokersToIgnore = append(brokersToIgnore, downedNode)
					continue
				}

				// here we have multiple ordinals that align to different nodes
				// and we're within our set ordinal range, make sure at least one
				// other node in the set is healthy and then we can mark this
				// node for decommission, otherwise, we can't distinguish which
				// pod is which broker (i.e. they're all down) and whether we
				// should actually decommission it or not
				hasHealthyBroker := false
				for _, broker := range brokers.Values() {
					if broker == downedNode {
						continue
					}
					if healthyNodes.HasAny(broker) {
						hasHealthyBroker = true
						break
					}
				}

				if hasHealthyBroker {
					// we have a healthy broker that isn't us, we can mark this for decommissioning
					brokersToDecommission = append(brokersToDecommission, downedNode)
					continue
				}

				// we can't tell which broker mapped to an ordinal is the current broker that
				// may actually correspond to a still-existing pod, so just ignore this broker
				brokersToIgnore = append(brokersToIgnore, downedNode)
				continue
			}

			if strings.Contains(err.Error(), "does not exist") {
				// delete the node from our sets
				downedNodes.Delete(downedNode)
				continue
			}

			log.Error(err, "fetching decommission status")
			return false, err
		}

		if status.Finished {
			// skip since we have already decommissioned it, so it should no longer
			// show up in the health overview
			continue
		}

		// add the brokers to the list of what needs to be decommissioned
		currentlyDecommissioningBrokers = append(currentlyDecommissioningBrokers, downedNode)
	}

	sortBrokers := func(set []int) []int {
		// sort by simple node id
		sort.SliceStable(set, func(i, j int) bool {
			return set[i] < set[j]
		})
		return set
	}

	healthyBrokers := sortBrokers(healthyNodes.Values())
	brokersToDecommission = sortBrokers(brokersToDecommission)
	brokersToIgnore = sortBrokers(brokersToIgnore)
	currentlyDecommissioningBrokers = sortBrokers(currentlyDecommissioningBrokers)

	formatBrokerList := func(set []int) string {
		return strings.Join(functional.MapFn(strconv.Itoa, set), ", ")
	}

	log.V(traceLevel).Info(fmt.Sprintf(
		"healthy brokers: [%s], ignored: [%s], to decommission: [%s], decommissioning: [%s]",
		formatBrokerList(healthyBrokers),
		formatBrokerList(brokersToIgnore),
		formatBrokerList(brokersToDecommission),
		formatBrokerList(currentlyDecommissioningBrokers),
	))

	if len(currentlyDecommissioningBrokers) != 0 {
		// we skip decommissioning our next broker since we already have some node decommissioning in progress
		return true, nil
	}

	if len(brokersToDecommission) > 0 {
		// only record the event here since this is when we trigger a decommission
		s.recorder.Eventf(set, corev1.EventTypeNormal, eventReasonBroker, "brokers needing decommissioning: [%s], decommissioning: %d", formatBrokerList(brokersToDecommission), brokersToDecommission[0])

		if err := adminClient.DecommissionBroker(ctx, brokersToDecommission[0]); err != nil {
			log.Error(err, "decommissioning broker", "broker", brokersToDecommission[0])
			return false, err
		}
	}

	// we should have decommissioned something above, so requeue and wait for it to finish
	return true, nil
}

// findUnboundVolumeClaims fetches any PVCs associated with the StatefulSet that aren't actively attached
// to a pod.
//
// Much of this code is copied from the original decommissioner and refactored, but the basic idea
// is:
//
// 1. Pull any pods matching the labels for the stateful set's pod template that are in the same namespace
// 2. Pull any pvcs matching the labels for the stateful set's volume claim template (though the component adds a "NAME-statefulset")
// 3. Find unbound volumes by checking that the pods we pulled reference every volume claim
//
// NB: this follow the original implementation that has a potential race-condition in the cache, where a PVC may come online and be in-cache
// but the corresponding pod has not yet populated into the cache. In this case the PVC could be marked for deletion
// despite the fact that it's still bound to a pod. In such a case the pvc-protection finalizer put in-place by core keeps the
// PVC from being deleted until the pod is deleted. Due to the skip of already-deleted PVCs below, these PVCs should
// just get GC'd when the pod is finally decommissioned.
//
// TODO: will this cause issues if a PVC is deleted and before it's GC'd a Pod comes up?
func (s *StatefulSetDecomissioner) findUnboundVolumeClaims(ctx context.Context, set *appsv1.StatefulSet) ([]*corev1.PersistentVolumeClaim, error) {
	pods := &corev1.PodList{}
	if err := s.client.List(ctx, pods, client.InNamespace(set.Namespace), client.MatchingLabels(set.Spec.Template.Labels)); err != nil {
		return nil, fmt.Errorf("listing pods: %w", err)
	}

	dataVolumeLabels := client.MatchingLabels{}
	for _, template := range set.Spec.VolumeClaimTemplates {
		if template.Name == datadirVolume {
			dataVolumeLabels = template.Labels
			break
		}
	}
	// the first part of this, "redpanda" is the component name (i.e. redpanda, console, etc.)
	dataVolumeLabels[k8sComponentLabelKey] = "redpanda-statefulset"

	// find all pvcs of the data directory for this StatefulSet
	pvcs := &corev1.PersistentVolumeClaimList{}
	if err := s.client.List(ctx, pvcs, client.InNamespace(set.Namespace), dataVolumeLabels); err != nil {
		return nil, fmt.Errorf("listing pvcs: %w", err)
	}

	unboundVolumes := map[string]*corev1.PersistentVolumeClaim{}
	for _, pvc := range pvcs.Items {
		// skip any pvcs that are already deleting
		if !pvc.DeletionTimestamp.IsZero() {
			continue
		}
		unboundVolumes[pvc.Name] = pvc.DeepCopy()
	}

	for _, pod := range pods.Items {
		for _, volume := range pod.Spec.Volumes {
			if volume.VolumeSource.PersistentVolumeClaim != nil {
				delete(unboundVolumes, volume.VolumeSource.PersistentVolumeClaim.ClaimName)
			}
		}
	}

	unbound := []*corev1.PersistentVolumeClaim{}
	for _, claim := range unboundVolumes {
		unbound = append(unbound, claim)
	}

	sort.SliceStable(unbound, func(i, j int) bool {
		first, second := unbound[i], unbound[j]
		if first.CreationTimestamp.Before(&second.CreationTimestamp) {
			return true
		}
		return first.Name < second.Name
	})

	return unbound, nil
}

// getAdminClient initializes an admin API client for a cluster that a statefulset manages. It does this by
// delegating to a "fetcher" which fetches the equivalent values.yaml map from either a Redpanda CR or an
// installed helm release. It then effectively turns this into a Redpanda CR that can be used for initializing
// clients based on existing factory code.
func (s *StatefulSetDecomissioner) getAdminClient(ctx context.Context, set *appsv1.StatefulSet) (*rpadmin.AdminAPI, error) {
	release, ok := set.Labels[k8sInstanceLabelKey]
	if !ok {
		return nil, errors.New("unable to get release name")
	}

	values, err := s.fetcher.FetchLatest(ctx, release, set.Namespace)
	if err != nil {
		return nil, fmt.Errorf("fetching latest values: %w", err)
	}

	data, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshaling values: %w", err)
	}

	cluster := &redpandav1alpha2.Redpanda{
		ObjectMeta: metav1.ObjectMeta{
			Name:      release,
			Namespace: set.Namespace,
		},
		Spec: redpandav1alpha2.RedpandaSpec{ClusterSpec: &redpandav1alpha2.RedpandaClusterSpec{}},
	}

	if err := json.Unmarshal(data, cluster.Spec.ClusterSpec); err != nil {
		return nil, fmt.Errorf("unmarshaling values: %w", err)
	}

	return s.factory.RedpandaAdminClient(ctx, cluster)
}

// ordinalFromFQDN takes a hostname and attempt to map the
// name back to a stateful set pod ordinal based on the left
// most DNS segment containing the form SETNAME-ORDINAL.
func ordinalFromFQDN(fqdn string) (int, error) {
	tokens := strings.Split(fqdn, ".")
	if len(tokens) < 2 {
		return 0, fmt.Errorf("invalid broker FQDN for ordinal fetching: %s", fqdn)
	}

	brokerPod := tokens[0]
	brokerTokens := strings.Split(brokerPod, "-")
	if len(brokerTokens) < 2 {
		return 0, fmt.Errorf("invalid broker FQDN for ordinal fetching: %s", fqdn)
	}

	// grab the last item after the "-"" which should be the ordinal and parse it
	ordinal, err := strconv.Atoi(brokerTokens[len(brokerTokens)-1])
	if err != nil {
		return 0, fmt.Errorf("parsing broker FQDN %q: %w", fqdn, err)
	}

	return ordinal, nil
}
