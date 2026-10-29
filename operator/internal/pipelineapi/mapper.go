// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package pipelineapi

import (
	"slices"
	"strings"

	dataplanev1 "buf.build/gen/go/redpandadata/dataplane/protocolbuffers/go/redpanda/api/dataplane/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

const (
	// The label and annotation match what the Console chart's admission
	// policy already keys on, so both designs share one notion of "managed".
	managedByLabel      = "console.redpanda.com/managed-by"
	managedByValue      = "console"
	createdByAnnotation = "console.redpanda.com/created-by"

	// cloudTagPrefix marks tags the Cloud UI reserves; they aren't stored.
	cloudTagPrefix = "__redpanda_cloud_"
)

// safeSpecFields are the only spec fields the API writes. crdDefaultedSpecFields
// are set by CRD defaulting on every Pipeline, so their presence doesn't make
// a Pipeline unmanageable; the API never changes them.
var (
	safeSpecFields         = []string{"displayName", "description", "tags", "configYaml", "resources", "paused", "cluster", "userRef"}
	crdDefaultedSpecFields = []string{"replicas", "updateStrategy"}
)

func isManaged(p *redpandav1alpha2.Pipeline) bool {
	return p.Labels[managedByLabel] == managedByValue
}

func pipelineToProto(p *redpandav1alpha2.Pipeline) *dataplanev1.Pipeline {
	state, errMsg := pipelineState(p)
	out := &dataplanev1.Pipeline{
		Id:          p.Name,
		DisplayName: p.Spec.DisplayName,
		Description: p.Spec.Description,
		ConfigYaml:  p.Spec.ConfigYAML,
		Tags:        p.Spec.Tags,
		State:       state,
		Resources:   resourcesToProto(p.Spec.Resources),
	}
	if out.DisplayName == "" {
		out.DisplayName = p.Name
	}
	if errMsg != "" {
		out.Status = &dataplanev1.Pipeline_Status{Error: errMsg}
	}
	// TODO: set created_by from createdByAnnotation, and create_time and
	// update_time from metadata, once the dataplane proto pin has them.
	return out
}

func resourcesToProto(r *corev1.ResourceRequirements) *dataplanev1.Pipeline_Resources {
	if r == nil || len(r.Requests) == 0 {
		return nil
	}
	out := &dataplanev1.Pipeline_Resources{}
	if q, ok := r.Requests[corev1.ResourceCPU]; ok {
		out.CpuShares = q.String()
	}
	if q, ok := r.Requests[corev1.ResourceMemory]; ok {
		out.MemoryShares = q.String()
	}
	return out
}

// pipelineState maps the controller's phase and conditions to the API state,
// plus the message of the failing condition, if any.
func pipelineState(p *redpandav1alpha2.Pipeline) (dataplanev1.Pipeline_State, string) {
	phase := p.Status.Phase
	conds := p.Status.Conditions
	errMsg := failureMessage(conds)

	// The controller hasn't seen the latest spec yet: report the transition
	// the user asked for instead of the stale phase.
	if p.Status.ObservedGeneration < p.Generation {
		if p.Spec.Paused && phase != redpandav1alpha2.PipelinePhaseStopped {
			return dataplanev1.Pipeline_STATE_STOPPING, errMsg
		}
		if !p.Spec.Paused && (phase == redpandav1alpha2.PipelinePhaseStopped || phase == "") {
			return dataplanev1.Pipeline_STATE_STARTING, errMsg
		}
	}

	if ready := findCondition(conds, redpandav1alpha2.PipelineConditionReady); ready != nil &&
		ready.Status == metav1.ConditionFalse && isFailureReason(ready.Reason) {
		return dataplanev1.Pipeline_STATE_ERROR, errMsg
	}

	switch phase {
	case redpandav1alpha2.PipelinePhasePending, redpandav1alpha2.PipelinePhaseProvisioning:
		if c := findCondition(conds, redpandav1alpha2.PipelineConditionConfigValid); c != nil && c.Status == metav1.ConditionFalse {
			return dataplanev1.Pipeline_STATE_ERROR, errMsg
		}
		return dataplanev1.Pipeline_STATE_STARTING, errMsg
	case redpandav1alpha2.PipelinePhaseRunning:
		return dataplanev1.Pipeline_STATE_RUNNING, errMsg
	case redpandav1alpha2.PipelinePhaseStopped:
		return dataplanev1.Pipeline_STATE_STOPPED, errMsg
	default:
		return dataplanev1.Pipeline_STATE_UNSPECIFIED, errMsg
	}
}

func isFailureReason(reason string) bool {
	switch reason {
	case "", redpandav1alpha2.PipelineReasonProvisioning, redpandav1alpha2.PipelineReasonRunning, redpandav1alpha2.PipelineReasonPaused:
		return false
	default:
		return true
	}
}

func failureMessage(conds []metav1.Condition) string {
	if ready := findCondition(conds, redpandav1alpha2.PipelineConditionReady); ready != nil &&
		ready.Status == metav1.ConditionFalse && isFailureReason(ready.Reason) {
		return ready.Message
	}
	for _, c := range conds {
		if c.Type != redpandav1alpha2.PipelineConditionReady && c.Status == metav1.ConditionFalse {
			return c.Message
		}
	}
	return ""
}

func findCondition(conds []metav1.Condition, condType string) *metav1.Condition {
	for i := range conds {
		if conds[i].Type == condType {
			return &conds[i]
		}
	}
	return nil
}

// desiredPipeline is the subset of a Pipeline the API owns.
type desiredPipeline struct {
	displayName string
	description string
	configYAML  string
	tags        map[string]string
	cpu, memory string
	paused      bool
}

func desiredFromPipeline(p *redpandav1alpha2.Pipeline) desiredPipeline {
	d := desiredPipeline{
		displayName: p.Spec.DisplayName,
		description: p.Spec.Description,
		configYAML:  p.Spec.ConfigYAML,
		tags:        p.Spec.Tags,
		paused:      p.Spec.Paused,
	}
	if r := resourcesToProto(p.Spec.Resources); r != nil {
		d.cpu, d.memory = r.CpuShares, r.MemoryShares
	}
	return d
}

// applyTo writes d, plus the configured cluster and user, onto p. Every other
// field is left as stored, which getManaged has already checked is only
// CRD-defaulted fields.
func (d desiredPipeline) applyTo(p *redpandav1alpha2.Pipeline, cfg Config) {
	p.Spec.DisplayName = d.displayName
	p.Spec.Description = d.description
	p.Spec.ConfigYAML = d.configYAML
	p.Spec.Paused = d.paused

	p.Spec.Tags = nil
	for k, v := range d.tags {
		if strings.HasPrefix(k, cloudTagPrefix) {
			continue
		}
		if p.Spec.Tags == nil {
			p.Spec.Tags = map[string]string{}
		}
		p.Spec.Tags[k] = v
	}

	requests := corev1.ResourceList{}
	if d.cpu != "" {
		requests[corev1.ResourceCPU] = resource.MustParse(d.cpu)
	}
	if d.memory != "" {
		requests[corev1.ResourceMemory] = resource.MustParse(d.memory)
	}
	// The API only edits requests; keep any cpu or memory limits an admin set.
	var limits corev1.ResourceList
	if p.Spec.Resources != nil {
		limits = p.Spec.Resources.Limits
	}
	p.Spec.Resources = nil
	if len(requests) > 0 || len(limits) > 0 {
		p.Spec.Resources = &corev1.ResourceRequirements{Limits: limits}
		if len(requests) > 0 {
			p.Spec.Resources.Requests = requests
		}
	}

	p.Spec.ClusterSource = nil
	if cfg.ClusterRef != "" {
		p.Spec.ClusterSource = &redpandav1alpha2.ClusterSource{ClusterRef: &redpandav1alpha2.ClusterRef{Name: cfg.ClusterRef}}
	}
	p.Spec.UserRef = nil
	if cfg.UserRef != "" {
		p.Spec.UserRef = &redpandav1alpha2.PipelineUserRef{Name: cfg.UserRef}
	}
}

// unmanageableSpecFields lists the set spec fields the API can't round-trip.
// A Pipeline with any of them (for example valueSources added by an admin)
// is read-only through the API, so an update can't silently drop them.
func unmanageableSpecFields(p *redpandav1alpha2.Pipeline, cfg Config) []string {
	spec, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&p.Spec)
	if err != nil {
		return []string{"spec"}
	}
	var out []string
	for k := range spec {
		if !slices.Contains(safeSpecFields, k) && !slices.Contains(crdDefaultedSpecFields, k) {
			out = append(out, "spec."+k)
		}
	}
	if cs := p.Spec.ClusterSource; cs != nil && (cs.StaticConfiguration != nil || cs.ClusterRef == nil ||
		cs.ClusterRef.Name != cfg.ClusterRef || cs.ClusterRef.Namespace != nil || cs.ClusterRef.Group != nil || cs.ClusterRef.Kind != nil) {
		out = append(out, "spec.cluster")
	}
	if p.Spec.UserRef != nil && p.Spec.UserRef.Name != cfg.UserRef {
		out = append(out, "spec.userRef")
	}
	if r := p.Spec.Resources; r != nil && (len(r.Claims) > 0 || hasNonComputeResources(r.Requests) || hasNonComputeResources(r.Limits)) {
		out = append(out, "spec.resources")
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func hasNonComputeResources(l corev1.ResourceList) bool {
	for name := range l {
		if name != corev1.ResourceCPU && name != corev1.ResourceMemory {
			return true
		}
	}
	return false
}
