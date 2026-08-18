// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package telemetry

import (
	"context"
	"fmt"
	"math"
	"runtime"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client"

	entv1alpha2 "github.com/redpanda-data/redpanda-operator/enterprise/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/api/apiutil"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
)

const (
	// redpandaCRDGroup is the API group for the Redpanda operator's
	// CustomResourceDefinitions.
	redpandaCRDGroup = "cluster.redpanda.com"
	// pvcNameLabel selects PersistentVolumeClaims owned by Redpanda clusters.
	pvcNameLabel = "app.kubernetes.io/name"
	// storageProvisionerAnnotation records the CSI driver that provisioned a
	// PersistentVolumeClaim.
	storageProvisionerAnnotation = "volume.kubernetes.io/storage-provisioner"
	// collectTimeout bounds a single collection cycle so a slow or unresponsive
	// API server cannot stall the reporter loop indefinitely.
	collectTimeout = 30 * time.Second
)

// Collector gathers anonymous cluster-shape telemetry about a Redpanda
// operator installation by listing the relevant Kubernetes resources.
type Collector struct {
	// Reader should be the manager's uncached API reader (mgr.GetAPIReader()),
	// not its cached client: this is a once-per-cycle read, so an informer cache
	// would be pure overhead (it would watch+cache every PVC and CRD in the
	// cluster for the process lifetime) and a forbidden read on the cached
	// client retries forever instead of failing fast.
	Reader          client.Reader
	ID              string
	OperatorVersion string
	// IDHash is the enterprise license checksum, included in the payload as
	// id_hash. Empty for OSS/unlicensed installs (then omitted).
	IDHash   string
	Features map[string]bool
	// Discovery resolves the Kubernetes server version each collection cycle so
	// the reported version tracks in-place cluster upgrades. Optional: when nil,
	// kubeVersion is omitted from the payload.
	Discovery discovery.ServerVersionInterface
	// ConnectDefaultImage is the operator-level Connect image override (the
	// --connect-default-image flag / connectController.image chart values).
	// Needed to resolve the effective image of Pipelines that don't pin
	// .spec.image — without it, fleets running the chart default would be
	// misreported as running the binary-baked constant.
	ConnectDefaultImage string
}

// Collect builds a Payload describing the current shape of the cluster.
//
// Collection degrades per field rather than all-or-nothing: a missing CRD
// (NoMatch) or missing RBAC (Forbidden) for one resource leaves that field at
// its zero value and collection continues, so installs that haven't yet applied
// a new CRD or its role rules still report everything else (operator version,
// the resources they do have) instead of vanishing from the dataset. Any other
// error (e.g. a transient API outage) is returned so the Reporter drops the
// cycle rather than sending a misleading all-zero payload.
func (c *Collector) Collect(ctx context.Context) (*Payload, error) {
	ctx, cancel := context.WithTimeout(ctx, collectTimeout)
	defer cancel()

	payload := &Payload{
		ID:              c.ID,
		OperatorVersion: c.OperatorVersion,
		GoVersion:       runtime.Version(),
		IDHash:          c.IDHash,
		Features:        c.Features,
	}

	// KubeVersion is best-effort: a discovery failure must not drop the whole
	// telemetry document, so on error the field is left empty (omitempty).
	if c.Discovery != nil {
		if info, err := c.Discovery.ServerVersion(); err == nil {
			payload.KubeVersion = info.GitVersion
		}
	}

	// Redpanda fleet. Process before NodePools so per-broker resources are known
	// for the NodePool correlation below.
	var rpSizing sizing
	// perBroker maps "namespace/name" -> a Redpanda's rendered per-broker
	// requirements, so NodePool CRs (which carry replicas but no resources of
	// their own) can be sized against their parent cluster.
	perBroker := map[string]corev1.ResourceRequirements{}
	var redpandas redpandav1alpha2.RedpandaList
	if ok, err := c.list(ctx, &redpandas); err != nil {
		return nil, err
	} else if ok {
		c.aggregateRedpandas(payload, redpandas.Items, &rpSizing, perBroker)
	}

	var nodePools redpandav1alpha2.NodePoolList
	if ok, err := c.list(ctx, &nodePools); err != nil {
		return nil, err
	} else if ok {
		payload.NodePools.Count = len(nodePools.Items)
		payload.NodePools.Enabled = payload.NodePools.Count > 0
		for i := range nodePools.Items {
			np := &nodePools.Items[i]
			replicas := derefInt32(np.Spec.Replicas)
			payload.Redpanda.BrokerCount += replicas
			// NodePool brokers inherit the parent Redpanda's per-broker resources.
			if reqs, found := perBroker[np.Namespace+"/"+np.Spec.ClusterRef.Name]; found {
				rpSizing.add(reqs, replicas)
			}
		}
	}
	rpSizing.into(&payload.Redpanda.TotalCPUCores, &payload.Redpanda.TotalMemoryGiB, &payload.Redpanda.BrokerSizes)

	var stretchClusters entv1alpha2.StretchClusterList
	if ok, err := c.list(ctx, &stretchClusters); err != nil {
		return nil, err
	} else if ok {
		payload.StretchCluster.Count = len(stretchClusters.Items)
		payload.StretchCluster.Enabled = payload.StretchCluster.Count > 0
		for i := range stretchClusters.Items {
			if tuning := stretchClusters.Items[i].Spec.Tuning; tuning != nil && ptrBool(tuning.ApplyHostTuners) {
				payload.StretchCluster.HostTuners++
			}
		}
	}

	// Stretch/multicluster broker sizing lives on RedpandaBrokerPool CRs.
	var brokerPools entv1alpha2.RedpandaBrokerPoolList
	if ok, err := c.list(ctx, &brokerPools); err != nil {
		return nil, err
	} else if ok {
		var sc sizing
		for i := range brokerPools.Items {
			pool := &brokerPools.Items[i]
			replicas := int(pool.GetReplicas())
			payload.StretchCluster.BrokerCount += replicas
			sc.add(pool.Spec.GetResourceRequirements(), replicas)
		}
		sc.into(&payload.StretchCluster.TotalCPUCores, &payload.StretchCluster.TotalMemoryGiB, &payload.StretchCluster.BrokerSizes)
	}

	var vectorized vectorizedv1alpha1.ClusterList
	if ok, err := c.list(ctx, &vectorized); err != nil {
		return nil, err
	} else if ok {
		payload.VectorizedClusters.Count = len(vectorized.Items)
		var vec sizing
		for i := range vectorized.Items {
			spec := vectorized.Items[i].Spec
			// Either the legacy single pool (spec.replicas) or explicit nodePools,
			// not both — avoid double counting.
			if len(spec.NodePools) == 0 {
				replicas := derefInt32(spec.Replicas)
				payload.VectorizedClusters.BrokerCount += replicas
				vec.add(spec.Resources.ResourceRequirements, replicas)
			} else {
				for _, np := range spec.NodePools {
					replicas := derefInt32(np.Replicas)
					payload.VectorizedClusters.BrokerCount += replicas
					vec.add(np.Resources.ResourceRequirements, replicas)
				}
			}
		}
		vec.into(&payload.VectorizedClusters.TotalCPUCores, &payload.VectorizedClusters.TotalMemoryGiB, &payload.VectorizedClusters.BrokerSizes)
	}

	// Supporting CR-type counts. Metadata-only lists: we only need len(), so
	// avoid loading full objects into memory.
	if err := c.count(ctx, redpandav1alpha2.SchemeGroupVersion.WithKind("TopicList"), &payload.Resources.Topics); err != nil {
		return nil, err
	}
	if err := c.count(ctx, redpandav1alpha2.SchemeGroupVersion.WithKind("UserList"), &payload.Resources.Users); err != nil {
		return nil, err
	}
	if err := c.count(ctx, redpandav1alpha2.SchemeGroupVersion.WithKind("SchemaList"), &payload.Resources.Schemas); err != nil {
		return nil, err
	}
	if err := c.count(ctx, redpandav1alpha2.SchemeGroupVersion.WithKind("RedpandaRoleList"), &payload.Resources.Roles); err != nil {
		return nil, err
	}
	if err := c.count(ctx, redpandav1alpha2.SchemeGroupVersion.WithKind("ShadowLinkList"), &payload.Resources.ShadowLinks); err != nil {
		return nil, err
	}
	// Consoles need a spec-aware list (not a metadata-only count) so we can
	// report how each Console exposes its UI — Gateway API HTTPRoute vs Ingress.
	var consoles redpandav1alpha2.ConsoleList
	if ok, err := c.list(ctx, &consoles); err != nil {
		return nil, err
	} else if ok {
		c.aggregateConsoles(payload, consoles.Items)
	}

	// CSI drivers come from a PVC *annotation*, so a metadata-only list
	// suffices — no need to load every Redpanda PVC's spec/status.
	var pvcs metav1.PartialObjectMetadataList
	pvcs.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("PersistentVolumeClaimList"))
	if ok, err := c.list(ctx, &pvcs, client.MatchingLabels{pvcNameLabel: "redpanda"}); err != nil {
		return nil, err
	} else if ok {
		drivers := map[string]struct{}{}
		for i := range pvcs.Items {
			if driver := pvcs.Items[i].Annotations[storageProvisionerAnnotation]; driver != "" {
				drivers[driver] = struct{}{}
			}
		}
		for driver := range drivers {
			payload.Storage.CSIDrivers = append(payload.Storage.CSIDrivers, driver)
		}
		sort.Strings(payload.Storage.CSIDrivers)
	}

	// CRD definitions are large; a metadata-only list keeps this cheap. A CRD's
	// name is always "<plural>.<group>", so the name suffix identifies the
	// group without loading specs.
	var crds metav1.PartialObjectMetadataList
	crds.SetGroupVersionKind(apiextensionsv1.SchemeGroupVersion.WithKind("CustomResourceDefinitionList"))
	if ok, err := c.list(ctx, &crds); err != nil {
		return nil, err
	} else if ok {
		for i := range crds.Items {
			if strings.HasSuffix(crds.Items[i].Name, "."+redpandaCRDGroup) {
				payload.CRDCount++
			}
		}
	}

	// Connect pipelines: is the Connect controller in use, at what scale.
	var pipelines redpandav1alpha2.PipelineList
	if ok, err := c.list(ctx, &pipelines); err != nil {
		return nil, err
	} else if ok {
		c.aggregatePipelines(payload, pipelines.Items)
	}

	// Node spread: count the distinct nodes the Connect pods are scheduled
	// across. Gated on having pipelines at all so installs with the CRD present
	// but no pipelines don't pay for a pod list every cycle.
	if payload.Connect.PipelineCount > 0 {
		if err := c.countConnectNodes(ctx, payload); err != nil {
			return nil, err
		}
	}

	return payload, nil
}

// aggregatePipelines folds the Pipeline fleet into the payload: count, paused /
// running counts, desired vs ready replicas, and the distinct image versions in
// use.
func (c *Collector) aggregatePipelines(payload *Payload, items []redpandav1alpha2.Pipeline) {
	payload.Connect.Enabled = true
	payload.Connect.PipelineCount = len(items)

	versions := map[string]struct{}{}
	for i := range items {
		p := &items[i]
		if p.Spec.Paused {
			payload.Connect.PausedPipelines++
		}
		if p.Status.Phase == redpandav1alpha2.PipelinePhaseRunning {
			payload.Connect.RunningPipelines++
		}
		// GetReplicas already returns 0 for paused pipelines, matching the pods
		// the operator is actually trying to run.
		payload.Connect.DesiredReplicas += int(p.GetReplicas())
		payload.Connect.ReadyReplicas += int(p.Status.ReadyReplicas)
		versions[imageVersion(c.resolvePipelineImage(p))] = struct{}{}
	}
	for v := range versions {
		payload.Connect.Versions = append(payload.Connect.Versions, v)
	}
	sort.Strings(payload.Connect.Versions)
}

// resolvePipelineImage mirrors the controller's three-tier image precedence:
// the Pipeline's own .spec.image, then the operator-level default
// (--connect-default-image), then the binary-baked constant.
func (c *Collector) resolvePipelineImage(p *redpandav1alpha2.Pipeline) string {
	if p.Spec.Image != nil && *p.Spec.Image != "" {
		return *p.Spec.Image
	}
	if c.ConnectDefaultImage != "" {
		return c.ConnectDefaultImage
	}
	return redpandav1alpha2.PipelineDefaultImage
}

// imageVersion reduces an image reference to just its version so the payload
// stays anonymous: full references can carry internal registry hostnames and
// team/project names. A tag (`repo:v1.2.3`) reports as the tag; a digest pin
// (`repo@sha256:...`) reports as a shortened digest; a bare repository reports
// as "unspecified".
func imageVersion(image string) string {
	if _, digest, ok := strings.Cut(image, "@"); ok {
		if len(digest) > 19 {
			digest = digest[:19] // "sha256:" + 12 hex chars
		}
		return digest
	}
	// Only treat a trailing :suffix as a tag when the colon comes after the
	// last path separator — `registry:5000/repo` has no tag.
	if i := strings.LastIndex(image, ":"); i > strings.LastIndex(image, "/") {
		return image[i+1:]
	}
	return "unspecified"
}

// countConnectNodes lists the Connect pods across all namespaces and records how
// many distinct nodes they are scheduled on. Best-effort: a missing-RBAC or
// NoMatch error leaves NodeCount at zero (per-field degradation).
func (c *Collector) countConnectNodes(ctx context.Context, payload *Payload) error {
	var pods corev1.PodList
	ok, err := c.list(ctx, &pods, client.MatchingLabels{
		"app.kubernetes.io/managed-by": "redpanda-operator",
		"app.kubernetes.io/component":  "connect-pipeline",
	})
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	nodes := map[string]struct{}{}
	for i := range pods.Items {
		if node := pods.Items[i].Spec.NodeName; node != "" {
			nodes[node] = struct{}{}
		}
	}
	payload.Connect.NodeCount = len(nodes)
	return nil
}

// aggregateRedpandas folds the Redpanda fleet into the payload: count, broker
// count, distinct versions, per-cluster feature adoption, and the default-pool
// sizing. Feature adoption and versions are read from the raw spec (explicit
// opt-ins), while broker count and resource sizing are derived from the
// chart-rendered values (cluster.GetValues()) so installs relying on chart
// defaults are not undercounted. perBroker is populated with each cluster's
// rendered per-broker requirements for NodePool correlation by the caller.
func (c *Collector) aggregateRedpandas(payload *Payload, items []redpandav1alpha2.Redpanda, rp *sizing, perBroker map[string]corev1.ResourceRequirements) {
	payload.Redpanda.Count = len(items)
	versions := map[string]struct{}{}
	for i := range items {
		rpCR := &items[i]
		spec := rpCR.Spec.ClusterSpec
		if spec == nil {
			continue
		}

		// Feature adoption + version: raw spec.
		if spec.Image != nil && spec.Image.Tag != nil && *spec.Image.Tag != "" {
			versions[*spec.Image.Tag] = struct{}{}
		}
		if spec.TLS != nil && ptrBool(spec.TLS.Enabled) {
			payload.Redpanda.TLS++
		}
		if spec.Auth != nil && spec.Auth.SASL != nil && ptrBool(spec.Auth.SASL.Enabled) {
			payload.Redpanda.SASL++
		}
		if spec.Storage != nil && spec.Storage.Tiered != nil && spec.Storage.Tiered.Config != nil &&
			jsonBoolTrue(spec.Storage.Tiered.Config.CloudStorageEnabled) {
			payload.Redpanda.TieredStorage++
		}
		if spec.Console != nil && ptrBool(spec.Console.Enabled) {
			payload.Redpanda.Console++
		}
		if spec.Connectors != nil && ptrBool(spec.Connectors.Enabled) {
			payload.Redpanda.ManagedConnectors++
		}
		// Mirror redpanda.ExternalConfig.IsGatewayEnabled(): a cluster only counts
		// as using Gateway API external access when external access is enabled, the
		// gateway block is enabled, AND at least one parentRef is set (otherwise no
		// TLSRoutes are rendered). Counting on gateway.Enabled alone overcounts
		// half-configured clusters that render no gateway resources.
		if spec.External != nil && ptrBool(spec.External.Enabled) &&
			spec.External.Gateway != nil && ptrBool(spec.External.Gateway.Enabled) &&
			len(spec.External.Gateway.ParentRefs) > 0 {
			payload.Redpanda.GatewayAPIExternalAccess++
		}
		// Host tuners are a pure opt-in (the chart defaults apply_host_tuners to
		// false), so the raw spec is the effective state — no need to consult the
		// rendered values. The always-on tune_aio_events tuner is deliberately not
		// counted; this tracks adoption of the chroot host-tuner mode only.
		if spec.Tuning != nil && ptrBool(spec.Tuning.ApplyHostTuners) {
			payload.Redpanda.HostTuners++
		}

		// Broker count + sizing: chart-rendered. Fall back to raw replicas for the
		// count if rendering fails (sizing is simply skipped for that cluster).
		values, err := rpCR.GetValues()
		if err != nil {
			if spec.Statefulset != nil && spec.Statefulset.Replicas != nil {
				payload.Redpanda.BrokerCount += *spec.Statefulset.Replicas
			}
			continue
		}
		replicas := int(values.Statefulset.Replicas)
		payload.Redpanda.BrokerCount += replicas
		reqs := values.Resources.GetResourceRequirements()
		rp.add(reqs, replicas)
		perBroker[rpCR.Namespace+"/"+rpCR.Name] = reqs
	}
	for v := range versions {
		payload.Redpanda.Versions = append(payload.Redpanda.Versions, v)
	}
	sort.Strings(payload.Redpanda.Versions)
}

// aggregateConsoles folds the Console CR fleet into the payload: the total count
// plus how each Console exposes its UI. The Console chart/CRD renders an
// HTTPRoute whenever spec.gateway.enabled is set (parentRefs are optional), so
// gateway.Enabled is the right signal for HTTPRoute adoption — unlike the
// Redpanda TLSRoute path, which additionally requires parentRefs to render.
func (c *Collector) aggregateConsoles(payload *Payload, items []redpandav1alpha2.Console) {
	payload.Resources.Consoles = len(items)
	for i := range items {
		spec := items[i].Spec
		if spec.Gateway != nil && ptrBool(spec.Gateway.Enabled) {
			payload.Console.HTTPRoute++
		}
		if spec.Ingress != nil && ptrBool(spec.Ingress.Enabled) {
			payload.Console.Ingress++
		}
	}
}

// sizing accumulates aggregate provisioned capacity (Σ per-broker × replicas)
// and the set of distinct per-broker sizes across a group of pools.
type sizing struct {
	milliCPU int64
	memBytes int64
	sizes    map[string]struct{}
}

// add folds one pool's per-broker requirements × replicas into the totals.
// Limits are preferred over Requests; a pool with neither CPU nor memory set is
// skipped (per-field degradation — unset resources contribute nothing).
func (s *sizing) add(reqs corev1.ResourceRequirements, replicas int) {
	if replicas <= 0 {
		return
	}
	list := reqs.Limits
	if len(list) == 0 {
		list = reqs.Requests
	}
	cpu := list[corev1.ResourceCPU]
	mem := list[corev1.ResourceMemory]
	milliCPU := cpu.MilliValue()
	memBytes := mem.Value()
	if milliCPU == 0 && memBytes == 0 {
		return
	}
	s.milliCPU += milliCPU * int64(replicas)
	s.memBytes += memBytes * int64(replicas)
	if s.sizes == nil {
		s.sizes = map[string]struct{}{}
	}
	s.sizes[brokerSizeLabel(milliCPU, memBytes)] = struct{}{}
}

// into writes the accumulated totals (rounded to integer cores/GiB) and the
// sorted distinct sizes into the payload fields.
func (s *sizing) into(cores, gib *int, sizes *[]string) {
	*cores = roundDiv(s.milliCPU, 1000)
	*gib = roundDiv(s.memBytes, 1<<30)
	if len(s.sizes) == 0 {
		return
	}
	out := make([]string, 0, len(s.sizes))
	for sz := range s.sizes {
		out = append(out, sz)
	}
	sort.Strings(out)
	*sizes = out
}

// brokerSizeLabel renders a per-broker size like "4c/16Gi" from raw millicores
// and bytes, rounded to integers.
func brokerSizeLabel(milliCPU, memBytes int64) string {
	return fmt.Sprintf("%dc/%dGi", roundDiv(milliCPU, 1000), roundDiv(memBytes, 1<<30))
}

func roundDiv(n, d int64) int { return int(math.Round(float64(n) / float64(d))) }

func derefInt32(p *int32) int {
	if p == nil {
		return 0
	}
	return int(*p)
}

// count lists the given object list (tolerating missing CRD/RBAC) and writes
// the item count into dst. It lists metadata only (PartialObjectMetadataList)
// so counting never loads full objects into memory.
func (c *Collector) count(ctx context.Context, listGVK schema.GroupVersionKind, dst *int) error {
	var list metav1.PartialObjectMetadataList
	list.SetGroupVersionKind(listGVK)
	ok, err := c.list(ctx, &list)
	if err != nil {
		return err
	}
	if ok {
		*dst = len(list.Items)
	}
	return nil
}

// list reads a resource list, tolerating the two permanent, install-specific
// conditions that would otherwise discard the whole telemetry document: a
// missing CRD (NoMatch) and missing RBAC (Forbidden). It returns ok=false (with
// a nil error) in those cases so the caller leaves the field at its zero value
// and continues; any other error is returned for the caller to propagate.
func (c *Collector) list(ctx context.Context, list client.ObjectList, opts ...client.ListOption) (bool, error) {
	if err := c.Reader.List(ctx, list, opts...); err != nil {
		if meta.IsNoMatchError(err) || apierrors.IsForbidden(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func ptrBool(b *bool) bool { return b != nil && *b }

// jsonBoolTrue reports whether a spec JSONBoolean is set to a truthy value.
// Truthiness matches JSONBoolean.MarshalJSON: the raw JSON is `true` or "true".
func jsonBoolTrue(b *apiutil.JSONBoolean) bool {
	if b == nil {
		return false
	}
	switch string(b.Raw) {
	case `true`, `"true"`:
		return true
	default:
		return false
	}
}
