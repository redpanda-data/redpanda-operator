package decommissioning

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/redpanda-data/common-go/rpadmin"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/kubernetes"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/collections"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/functional"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	appsv1ac "k8s.io/client-go/applyconfigurations/apps/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	decommissionCondition appsv1.StatefulSetConditionType = "Decommissioning"

	reasonConditionNoNeed                = "NoNeed"
	reasonConditionWaiting               = "Waiting"
	reasonConditionBrokerDecommissioning = "BrokerDecommissioning"
	reasonConditionPVCDecommissioning    = "PersistentVolumeClaimDecommissioning"
	reasonConditionError                 = "Error"

	eventDecommissioningBroker = "DecommissioningBroker"
	eventReasonBrokerGone      = "BrokerGone"

	eventDecommissioningPersistentVolumeClaim = "DecommissioningPersistentVolumeClaim"
	eventReasonUnboundPersistentVolumeClaims  = "UnboundPersistentVolumeClaims"

	k8sManagedByLabelKey = "app.kubernetes.io/managed-by"
	k8sInstanceLabelKey  = "app.kubernetes.io/instance"
	k8sComponentLabelKey = "app.kubernetes.io/component"
	datadirVolume        = "datadir"

	traceLevel = 2
	debugLevel = 1
	infoLevel  = 0
)

type Option func(*StatefulSetDecomissioner)

func FilterStatefulSetOwner(ownerNamespace, ownerName string) func(ctx context.Context, set *appsv1.StatefulSet) (bool, error) {
	filter := FilterOwner(ownerNamespace, ownerName)
	return func(ctx context.Context, set *appsv1.StatefulSet) (bool, error) {
		return filter(set), nil
	}
}

func FilterOwner(ownerNamespace, ownerName string) func(o client.Object) bool {
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

type StatefulSetDecomissioner struct {
	client   client.Client
	factory  internalclient.ClientFactory
	fetcher  ValuesFetcher
	recorder record.EventRecorder
	filter   func(ctx context.Context, set *appsv1.StatefulSet) (bool, error)
}

func NewStatefulSetDecommissioner(mgr ctrl.Manager, fetcher ValuesFetcher, options ...Option) *StatefulSetDecomissioner {
	k8sClient := mgr.GetClient()

	decommissioner := &StatefulSetDecomissioner{
		recorder: mgr.GetEventRecorderFor("BrokerDecommissioner"),
		client:   k8sClient,
		fetcher:  fetcher,
		factory:  internalclient.NewFactory(mgr.GetConfig(), k8sClient),
		filter:   func(ctx context.Context, set *appsv1.StatefulSet) (bool, error) { return true, nil },
	}

	for _, opt := range options {
		opt(decommissioner)
	}

	return decommissioner
}

func (s *StatefulSetDecomissioner) Decommission(ctx context.Context, set *appsv1.StatefulSet) (*appsv1ac.StatefulSetConditionApplyConfiguration, bool, error) {
	// note that this is best-effort, the decommissioning code needs to be idempotent and deterministic

	log := ctrl.LoggerFrom(ctx, "namespace", set.Namespace, "name", set.Name).WithName("StatefulSetDecommissioner.Decomission")

	// if helm is not managing it, move on.
	if managedBy, ok := set.Labels[k8sManagedByLabelKey]; managedBy != "Helm" || !ok {
		log.V(traceLevel).Info("not managed by helm")
		return nil, false, nil
	}

	keep, err := s.filter(ctx, set)
	if err != nil {
		log.Error(err, "error filtering StatefulSet")
		return nil, false, err
	}

	if !keep {
		log.V(traceLevel).Info("skipping decommission, StatefulSet filtered out")
		return nil, false, nil
	}

	unboundVolumeClaims, err := s.findUnboundVolumeClaims(ctx, set)
	if err != nil {
		log.Error(err, "error finding unbound PersistentVolumeClaims")
		return nil, false, err
	}

	log.V(traceLevel).Info("fetched unbound volume claims", "claims", functional.MapFn(func(claim *corev1.PersistentVolumeClaim) string {
		return claim.Name
	}, unboundVolumeClaims))

	condition := &appsv1ac.StatefulSetConditionApplyConfiguration{
		Type: ptr.To(decommissionCondition),
	}

	setCondition := func(requeue bool, err error) (*appsv1ac.StatefulSetConditionApplyConfiguration, bool, error) {
		if err != nil {
			return condition.WithStatus(corev1.ConditionFalse).WithReason(reasonConditionError).WithMessage(err.Error()), false, err
		}

		return condition, requeue, err
	}

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
			return setCondition(false, err)
		}

		// now that we've patched the PV, delete the PVC
		if err := s.client.Delete(ctx, claim); err != nil {
			log.Error(err, "error deleting PersistentVolumeClaim")
			return setCondition(false, err)
		}

		message := fmt.Sprintf(
			"unbound persistent volumes: [%s], decommissioning: %s", strings.Join(functional.MapFn(func(claim *corev1.PersistentVolumeClaim) string {
				return client.ObjectKeyFromObject(claim).String()
			}, unboundVolumeClaims), ", "), client.ObjectKeyFromObject(claim).String(),
		)
		condition = condition.WithMessage(message).WithReason(reasonConditionPVCDecommissioning).WithStatus(corev1.ConditionTrue)

		s.recorder.Eventf(set, eventDecommissioningPersistentVolumeClaim, eventReasonUnboundPersistentVolumeClaims, message)

		return setCondition(false, nil)
	}

	// now we check if we can/should decommission any brokers
	adminClient, err := s.getAdminClient(ctx, set)
	if err != nil {
		log.Error(err, "initializing admin client")
		return setCondition(false, err)
	}

	health, err := adminClient.GetHealthOverview(ctx)
	if err != nil {
		log.Error(err, "fetching brokers")
		return setCondition(false, err)
	}

	requestedNodes := int(ptr.Deref(set.Spec.Replicas, 0))
	if len(health.AllNodes) <= requestedNodes {
		// we don't need to decommission anything since we're at the proper
		// capacity
		condition = condition.WithStatus(corev1.ConditionFalse).WithReason(reasonConditionNoNeed).
			WithMessage("cluster does not have any nodes which need decommissioning")

		return setCondition(false, nil)
	}

	if len(health.NodesDown) == 0 {
		// we don't need to decommission anything since everything is healthy
		// and we want to wait until a broker is fully stopped
		condition = condition.WithStatus(corev1.ConditionFalse).WithReason(reasonConditionWaiting).
			WithMessage("waiting for StatefulSet to delete pod before beginning decommission")

		return setCondition(false, nil)
	}

	allNodes := collections.NewSet[int]()
	allNodes.Add(health.AllNodes...)
	downedNodes := collections.NewSet[int]()
	downedNodes.Add(health.NodesDown...)
	healthyNodes := allNodes.LeftDisjoint(downedNodes)

	brokersToDecommission := []int{}
	currentlyDecommissioningBrokers := []int{}

	for _, downedNode := range health.NodesDown {
		status, err := adminClient.DecommissionBrokerStatus(ctx, downedNode)
		if err != nil {
			if strings.Contains(err.Error(), "is not decommissioning") {
				brokersToDecommission = append(brokersToDecommission, downedNode)
				continue
			}

			if strings.Contains(err.Error(), "does not exist") {
				// delete the node from our sets
				downedNodes.Delete(downedNode)
				continue
			}

			log.Error(err, "fetching decommission status")
			return setCondition(false, err)
		}

		if status.Finished {
			// TODO: does this seem right?
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
	currentlyDecommissioningBrokers = sortBrokers(currentlyDecommissioningBrokers)

	formatBrokerList := func(set []int) string {
		return strings.Join(functional.MapFn(strconv.Itoa, set), ", ")
	}

	message := fmt.Sprintf(
		"healthy brokers: [%s], to decommission: [%s], decommissioning: [%s]",
		formatBrokerList(healthyBrokers),
		formatBrokerList(brokersToDecommission),
		formatBrokerList(currentlyDecommissioningBrokers),
	)
	condition = condition.WithMessage(message).WithReason(reasonConditionBrokerDecommissioning).WithStatus(corev1.ConditionTrue)

	if len(currentlyDecommissioningBrokers) != 0 {
		// we skip decommissioing our next broker since we already have some node decommissioning in progress
		return setCondition(true, nil)
	}

	if len(brokersToDecommission) > 0 {
		s.recorder.Eventf(set, eventDecommissioningBroker, eventReasonBrokerGone, message)

		if err := adminClient.DecommissionBroker(ctx, brokersToDecommission[0]); err != nil {
			log.Error(err, "decommissioning broker", "broker", brokersToDecommission[0])
			return setCondition(false, err)
		}
	}

	// we should have decommissioned something above, so requeue and wait for it to finish
	return setCondition(true, nil)
}

func (s *StatefulSetDecomissioner) findUnboundVolumeClaims(ctx context.Context, set *appsv1.StatefulSet) ([]*corev1.PersistentVolumeClaim, error) {
	pods := &corev1.PodList{}
	if err := s.client.List(ctx, pods, client.InNamespace(set.Namespace), client.MatchingLabels(set.Spec.Template.Labels)); err != nil {
		return nil, fmt.Errorf("listing pods: %w", err)
	}

	dataVolumeLabels := client.MatchingLabels{}
	for _, template := range set.Spec.VolumeClaimTemplates {
		if template.Name == datadirVolume {
			for name, label := range template.Labels {
				dataVolumeLabels[name] = label
				if name == k8sComponentLabelKey {
					dataVolumeLabels[name] = fmt.Sprintf("%s-statefulset", label)
				}
			}
			break
		}
	}

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
