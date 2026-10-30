// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package pipelineapi serves Redpanda Connect's dataplane.v1 PipelineService
// from Pipeline resources in one namespace. It's the self-hosted counterpart
// of Redpanda Cloud's connect-api: Redpanda Console proxies its pipeline
// calls here instead of talking to the Kubernetes API itself.
package pipelineapi

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"buf.build/gen/go/redpandadata/dataplane/connectrpc/go/redpanda/api/dataplane/v1/dataplanev1connect"
	dataplanev1 "buf.build/gen/go/redpandadata/dataplane/protocolbuffers/go/redpanda/api/dataplane/v1"
	"connectrpc.com/connect"
	"github.com/cockroachdb/errors"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

const (
	fieldManager      = "redpanda-pipeline-api"
	defaultPageSize   = 100
	maxCreateAttempts = 3

	maxNameLen   = 63
	suffixLen    = 5
	maxSlugLen   = maxNameLen - suffixLen - 1
	fallbackSlug = "pipeline"
)

// Config scopes the API to one namespace and pins the identity of every
// pipeline it writes. Callers never choose the cluster or the user.
type Config struct {
	Namespace  string
	ClusterRef string
	UserRef    string
}

// Service implements dataplanev1connect.PipelineServiceHandler. Component
// listing, linting and the config schema stay in Console, which bundles
// Redpanda Connect's schema; they return Unimplemented here.
type Service struct {
	dataplanev1connect.UnimplementedPipelineServiceHandler

	cfg       Config
	client    client.Client
	logger    *slog.Logger
	newSuffix func() string
}

var _ dataplanev1connect.PipelineServiceHandler = (*Service)(nil)

// NewService returns a Service. c must not be a cached client: List relies
// on API server pagination.
func NewService(cfg Config, c client.Client, logger *slog.Logger) (*Service, error) {
	if cfg.Namespace == "" {
		return nil, errors.New("namespace is required")
	}
	if cfg.ClusterRef != "" && cfg.UserRef == "" {
		return nil, errors.New("userRef is required with clusterRef, otherwise pipelines authenticate as the bootstrap superuser")
	}
	return &Service{
		cfg:       cfg,
		client:    c,
		logger:    logger,
		newSuffix: func() string { return rand.String(suffixLen) },
	}, nil
}

// ListPipelines lists the namespace's Pipelines, including ones created with
// kubectl or GitOps, which are read-only through this API.
func (s *Service) ListPipelines(ctx context.Context, req *connect.Request[dataplanev1.ListPipelinesRequest]) (*connect.Response[dataplanev1.ListPipelinesResponse], error) {
	f := req.Msg.GetFilter()

	var pageSize int64
	switch size := req.Msg.GetPageSize(); {
	case size > 0:
		pageSize = int64(size)
	case size == 0:
		pageSize = defaultPageSize
	}

	// Filters apply after API server pagination, so with a filter keep
	// listing until the page is full, never asking for more items than
	// still fit, so the continue token skips nothing.
	filtered := f.GetNameContains() != "" || len(f.GetTags()) > 0
	out := make([]*dataplanev1.Pipeline, 0)
	token := req.Msg.GetPageToken()
	for {
		limit := pageSize
		if filtered && pageSize > 0 {
			limit = pageSize - int64(len(out))
		}
		var list redpandav1alpha2.PipelineList
		if err := s.client.List(ctx, &list, client.InNamespace(s.cfg.Namespace), client.Limit(limit), client.Continue(token)); err != nil {
			return nil, s.kubeError(ctx, err, "failed to list pipelines", "")
		}
		for i := range list.Items {
			if p := pipelineToProto(&list.Items[i]); matchesFilter(p, f) {
				out = append(out, p)
			}
		}
		token = list.Continue
		if !filtered || token == "" || (pageSize > 0 && int64(len(out)) >= pageSize) {
			return connect.NewResponse(&dataplanev1.ListPipelinesResponse{Pipelines: out, NextPageToken: token}), nil
		}
	}
}

// GetPipeline returns one Pipeline.
func (s *Service) GetPipeline(ctx context.Context, req *connect.Request[dataplanev1.GetPipelineRequest]) (*connect.Response[dataplanev1.GetPipelineResponse], error) {
	p, err := s.get(ctx, req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&dataplanev1.GetPipelineResponse{Pipeline: pipelineToProto(p)}), nil
}

// CreatePipeline creates a managed Pipeline from the safe field subset.
func (s *Service) CreatePipeline(ctx context.Context, req *connect.Request[dataplanev1.CreatePipelineRequest]) (*connect.Response[dataplanev1.CreatePipelineResponse], error) {
	in := req.Msg.GetPipeline()
	if in == nil {
		return nil, newInvalidArgument("pipeline", "pipeline is required")
	}
	if err := checkConfigPolicy(in.GetConfigYaml()); err != nil {
		return nil, err
	}
	cpu, memory, err := normalizeResources(in.GetResources())
	if err != nil {
		return nil, err
	}
	desired := desiredPipeline{
		displayName: in.GetDisplayName(),
		description: in.GetDescription(),
		configYAML:  in.GetConfigYaml(),
		tags:        in.GetTags(),
		cpu:         cpu,
		memory:      memory,
	}

	for range maxCreateAttempts {
		p := &redpandav1alpha2.Pipeline{ObjectMeta: metav1.ObjectMeta{
			Name:      nameFromDisplayName(in.GetDisplayName(), s.newSuffix()),
			Namespace: s.cfg.Namespace,
			Labels:    map[string]string{managedByLabel: managedByValue},
		}}
		if principal, ok := PrincipalFromContext(ctx); ok {
			p.Annotations = map[string]string{createdByAnnotation: principal}
		}
		desired.applyTo(p, s.cfg)

		err := s.client.Create(ctx, p, client.FieldOwner(fieldManager))
		if k8serrors.IsAlreadyExists(err) {
			continue
		}
		if err != nil {
			return nil, s.kubeError(ctx, err, "failed to create pipeline", p.Name)
		}
		s.audit(ctx, "create", p.Name)
		return connect.NewResponse(&dataplanev1.CreatePipelineResponse{Pipeline: pipelineToProto(p)}), nil
	}
	return nil, connect.NewError(connect.CodeAborted, fmt.Errorf("failed to find a free pipeline name after %d attempts", maxCreateAttempts))
}

// UpdatePipeline changes the safe fields of a managed Pipeline.
func (s *Service) UpdatePipeline(ctx context.Context, req *connect.Request[dataplanev1.UpdatePipelineRequest]) (*connect.Response[dataplanev1.UpdatePipelineResponse], error) {
	in := req.Msg.GetPipeline()
	if in == nil {
		return nil, newInvalidArgument("pipeline", "pipeline is required")
	}
	p, err := s.getManaged(ctx, req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	desired := desiredFromPipeline(p)
	if err := applyUpdate(&desired, in, req.Msg.GetUpdateMask().GetPaths()); err != nil {
		return nil, err
	}
	if err := s.write(ctx, p, desired); err != nil {
		return nil, err
	}
	s.audit(ctx, "update", p.Name)
	return connect.NewResponse(&dataplanev1.UpdatePipelineResponse{Pipeline: pipelineToProto(p)}), nil
}

func applyUpdate(desired *desiredPipeline, in *dataplanev1.PipelineUpdate, paths []string) error {
	for _, path := range paths {
		switch path {
		case "display_name", "description", "config_yaml", "tags", "resources", "resources.cpu_shares", "resources.memory_shares":
		case "service_account", "draft":
			// Accepted and ignored: Kubernetes pipelines have no Cloud service
			// account, and no drafts.
		default:
			return newInvalidArgument("update_mask", fmt.Sprintf("unsupported update_mask path %q", path))
		}
	}
	all := len(paths) == 0
	set := func(field string) bool { return all || slices.Contains(paths, field) }

	if set("display_name") {
		desired.displayName = in.GetDisplayName()
	}
	if set("description") {
		desired.description = in.GetDescription()
	}
	if set("tags") {
		desired.tags = in.GetTags()
	}
	if set("config_yaml") {
		if err := checkConfigPolicy(in.GetConfigYaml()); err != nil {
			return err
		}
		desired.configYAML = in.GetConfigYaml()
	}
	// A full update without resources keeps the stored ones.
	if in.GetResources() == nil && all {
		return nil
	}
	cpu, memory, err := normalizeResources(in.GetResources())
	if err != nil {
		return err
	}
	if set("resources") || set("resources.cpu_shares") {
		desired.cpu = cpu
	}
	if set("resources") || set("resources.memory_shares") {
		desired.memory = memory
	}
	return nil
}

// StopPipeline pauses a managed Pipeline.
func (s *Service) StopPipeline(ctx context.Context, req *connect.Request[dataplanev1.StopPipelineRequest]) (*connect.Response[dataplanev1.StopPipelineResponse], error) {
	p, err := s.setPaused(ctx, req.Msg.GetId(), true)
	if err != nil {
		return nil, err
	}
	s.audit(ctx, "stop", p.Name)
	return connect.NewResponse(&dataplanev1.StopPipelineResponse{Pipeline: pipelineToProto(p)}), nil
}

// StartPipeline resumes a managed Pipeline.
func (s *Service) StartPipeline(ctx context.Context, req *connect.Request[dataplanev1.StartPipelineRequest]) (*connect.Response[dataplanev1.StartPipelineResponse], error) {
	p, err := s.setPaused(ctx, req.Msg.GetId(), false)
	if err != nil {
		return nil, err
	}
	s.audit(ctx, "start", p.Name)
	return connect.NewResponse(&dataplanev1.StartPipelineResponse{Pipeline: pipelineToProto(p)}), nil
}

func (s *Service) setPaused(ctx context.Context, id string, paused bool) (*redpandav1alpha2.Pipeline, error) {
	p, err := s.getManaged(ctx, id)
	if err != nil {
		return nil, err
	}
	desired := desiredFromPipeline(p)
	desired.paused = paused
	if err := s.write(ctx, p, desired); err != nil {
		return nil, err
	}
	return p, nil
}

// DeletePipeline deletes a managed Pipeline, if it hasn't changed since it
// was checked.
func (s *Service) DeletePipeline(ctx context.Context, req *connect.Request[dataplanev1.DeletePipelineRequest]) (*connect.Response[dataplanev1.DeletePipelineResponse], error) {
	p, err := s.getManaged(ctx, req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	if err := s.client.Delete(ctx, p, client.Preconditions{UID: &p.UID, ResourceVersion: &p.ResourceVersion}); err != nil {
		return nil, s.kubeError(ctx, err, "failed to delete pipeline", p.Name)
	}
	s.audit(ctx, "delete", p.Name)
	return connect.NewResponse(&dataplanev1.DeletePipelineResponse{}), nil
}

// GetPipelinesForSecret returns no pipelines: secrets are out of scope.
func (*Service) GetPipelinesForSecret(_ context.Context, req *connect.Request[dataplanev1.GetPipelinesForSecretRequest]) (*connect.Response[dataplanev1.GetPipelinesForSecretResponse], error) {
	return connect.NewResponse(&dataplanev1.GetPipelinesForSecretResponse{
		PipelinesForSecret: &dataplanev1.PipelinesForSecret{SecretId: req.Msg.GetSecretId()},
	}), nil
}

// GetPipelinesBySecrets returns no pipelines: secrets are out of scope.
func (*Service) GetPipelinesBySecrets(context.Context, *connect.Request[dataplanev1.GetPipelinesBySecretsRequest]) (*connect.Response[dataplanev1.GetPipelinesBySecretsResponse], error) {
	return connect.NewResponse(&dataplanev1.GetPipelinesBySecretsResponse{}), nil
}

func (s *Service) get(ctx context.Context, id string) (*redpandav1alpha2.Pipeline, error) {
	if errs := validation.IsDNS1123Subdomain(id); len(errs) > 0 {
		return nil, newInvalidArgument("id", fmt.Sprintf("invalid pipeline id %q: %s", id, strings.Join(errs, "; ")))
	}
	var p redpandav1alpha2.Pipeline
	if err := s.client.Get(ctx, client.ObjectKey{Namespace: s.cfg.Namespace, Name: id}, &p); err != nil {
		return nil, s.kubeError(ctx, err, "failed to get pipeline", id)
	}
	return &p, nil
}

// getManaged returns a Pipeline the API may change: one it labeled, whose
// stored spec has nothing outside what the API writes back.
func (s *Service) getManaged(ctx context.Context, id string) (*redpandav1alpha2.Pipeline, error) {
	p, err := s.get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !isManaged(p) {
		return nil, newNotManagedError(p.Name)
	}
	if fields := unmanageableSpecFields(p, s.cfg); len(fields) > 0 {
		return nil, newUnmanageableFieldsError(p.Name, fields)
	}
	return p, nil
}

// write updates p in place. p carries the resourceVersion it was read at, so
// a concurrent change fails with a conflict instead of being overwritten.
func (s *Service) write(ctx context.Context, p *redpandav1alpha2.Pipeline, desired desiredPipeline) error {
	desired.applyTo(p, s.cfg)
	if err := s.client.Update(ctx, p, client.FieldOwner(fieldManager)); err != nil {
		return s.kubeError(ctx, err, "failed to update pipeline", p.Name)
	}
	return nil
}

func (s *Service) audit(ctx context.Context, action, pipelineID string) {
	logger := s.logger
	if principal, ok := PrincipalFromContext(ctx); ok {
		logger = logger.With(slog.String("principal", principal))
	}
	if caller, ok := CallerFromContext(ctx); ok {
		logger = logger.With(slog.String("caller", caller))
	}
	logger.InfoContext(ctx, "pipeline changed", slog.String("action", action), slog.String("pipeline_id", pipelineID))
}

func normalizeResources(r *dataplanev1.Pipeline_Resources) (cpu, memory string, err error) {
	if cpu, err = normalizeQuantity("resources.cpu_shares", r.GetCpuShares()); err != nil {
		return "", "", err
	}
	if memory, err = normalizeQuantity("resources.memory_shares", r.GetMemoryShares()); err != nil {
		return "", "", err
	}
	return cpu, memory, nil
}

// normalizeQuantity returns value in canonical form, and "" for unset or zero.
func normalizeQuantity(field, value string) (string, error) {
	if value == "" {
		return "", nil
	}
	q, err := resource.ParseQuantity(value)
	if err != nil {
		return "", newInvalidArgument(field, fmt.Sprintf("invalid quantity %q: %v", value, err))
	}
	if q.Sign() < 0 {
		return "", newInvalidArgument(field, fmt.Sprintf("invalid quantity %q: must not be negative", value))
	}
	if q.IsZero() {
		return "", nil
	}
	return q.String(), nil
}

func matchesFilter(p *dataplanev1.Pipeline, f *dataplanev1.ListPipelinesRequest_Filter) bool {
	if f == nil {
		return true
	}
	if nc := strings.ToLower(f.GetNameContains()); nc != "" &&
		!strings.Contains(strings.ToLower(p.GetDisplayName()), nc) && !strings.Contains(p.GetId(), nc) {
		return false
	}
	for k, v := range f.GetTags() {
		if got, ok := p.GetTags()[k]; !ok || got != v {
			return false
		}
	}
	return true
}

// nameFromDisplayName returns a DNS-1123 name: a slug of displayName (or
// "pipeline") plus "-" and suffix, at most 63 characters.
func nameFromDisplayName(displayName, suffix string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(displayName) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	slug := b.String()
	if len(slug) > maxSlugLen {
		slug = slug[:maxSlugLen]
	}
	slug = strings.Trim(slug, "-")
	if slug == "" || slug[0] < 'a' || slug[0] > 'z' {
		slug = strings.Trim(fallbackSlug+"-"+slug, "-")
		if len(slug) > maxSlugLen {
			slug = strings.TrimRight(slug[:maxSlugLen], "-")
		}
	}
	return slug + "-" + suffix
}
