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
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"buf.build/gen/go/redpandadata/dataplane/connectrpc/go/redpanda/api/dataplane/v1/dataplanev1connect"
	dataplanev1 "buf.build/gen/go/redpandadata/dataplane/protocolbuffers/go/redpanda/api/dataplane/v1"
	"connectrpc.com/connect"
	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/common-go/kube"
	"github.com/redpanda-data/common-go/kube/kubetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	crds "github.com/redpanda-data/redpanda-operator/operator/config/crd/bases"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	"github.com/redpanda-data/redpanda-operator/operator/internal/testutils"
)

const (
	testNamespace = "pipelines"
	consoleCaller = "system:serviceaccount:redpanda:console"
	consoleToken  = "console-token"
	otherToken    = "other-token"

	validConfig = "input:\n  generate:\n    mapping: root = \"hello\"\n    interval: 1s\noutput:\n  drop: {}\n"
)

// staticReviewer stands in for TokenReview: it knows two tokens.
type staticReviewer struct{ calls atomic.Int32 }

func (r *staticReviewer) Review(_ context.Context, token string) (string, error) {
	r.calls.Add(1)
	switch token {
	case consoleToken:
		return consoleCaller, nil
	case otherToken:
		return "system:serviceaccount:default:other", nil
	}
	return "", errUnauthenticated
}

// newProxyClient builds a client the way console-enterprise's pipeline proxy
// does (gRPC over unencrypted HTTP/2), adding the headers Console would send.
func newProxyClient(url, token, principal string) dataplanev1connect.PipelineServiceClient {
	protocols := &http.Protocols{}
	protocols.SetUnencryptedHTTP2(true)
	hc := &http.Client{Transport: &http.Transport{Protocols: protocols}, Timeout: 30 * time.Second}
	headers := connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			if token != "" {
				req.Header().Set("Authorization", "Bearer "+token)
			}
			if principal != "" {
				req.Header().Set(PrincipalHeader, principal)
			}
			return next(ctx, req)
		}
	})
	return dataplanev1connect.NewPipelineServiceClient(hc, url, connect.WithGRPC(), connect.WithInterceptors(headers))
}

func TestPipelineAPI(t *testing.T) {
	ctx := t.Context()

	ctl := kubetest.NewEnv(t, kube.Options{Options: client.Options{Scheme: controller.UnifiedScheme}})
	testutils.InstallCRDs(t, ctl, crds.All()...)
	c, err := client.New(ctl.RestConfig(), client.Options{Scheme: controller.V2Scheme})
	require.NoError(t, err)
	require.NoError(t, c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace}}))

	cfg := Config{Namespace: testNamespace, ClusterRef: "redpanda", UserRef: "console-pipelines"}
	svc, err := NewService(cfg, c, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	reviewer := &staticReviewer{}
	auth, err := NewAuthInterceptor(AuthOptions{Reviewer: reviewer, AllowedCallers: []string{consoleCaller}})
	require.NoError(t, err)

	srv := httptest.NewUnstartedServer(Handler(svc, auth))
	srv.Config.Protocols = &http.Protocols{}
	srv.Config.Protocols.SetHTTP1(true)
	srv.Config.Protocols.SetUnencryptedHTTP2(true)
	srv.Config.BaseContext = func(net.Listener) context.Context { return ctx }
	srv.Start()
	t.Cleanup(srv.Close)

	api := newProxyClient(srv.URL, consoleToken, "alice@example.com")

	// A pipeline applied with kubectl: listed, but read-only to the API.
	external := &redpandav1alpha2.Pipeline{
		ObjectMeta: metav1.ObjectMeta{Name: "gitops-orders", Namespace: testNamespace},
		Spec:       redpandav1alpha2.PipelineSpec{ConfigYAML: validConfig},
	}
	require.NoError(t, c.Create(ctx, external))

	var id string
	t.Run("create", func(t *testing.T) {
		resp, err := api.CreatePipeline(ctx, connect.NewRequest(&dataplanev1.CreatePipelineRequest{Pipeline: &dataplanev1.PipelineCreate{
			DisplayName: "Orders to S3",
			ConfigYaml:  validConfig,
			Tags:        map[string]string{"team": "data", "__redpanda_cloud_hidden": "x"},
			Resources:   &dataplanev1.Pipeline_Resources{CpuShares: "200m", MemoryShares: "256Mi"},
		}}))
		require.NoError(t, err)
		id = resp.Msg.GetPipeline().GetId()
		assert.Regexp(t, `^orders-to-s3-[a-z0-9]{5}$`, id)

		var p redpandav1alpha2.Pipeline
		require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: id}, &p))
		assert.Equal(t, managedByValue, p.Labels[managedByLabel])
		assert.Equal(t, "alice@example.com", p.Annotations[createdByAnnotation])
		assert.Equal(t, "redpanda", p.Spec.ClusterSource.ClusterRef.Name)
		assert.Equal(t, "console-pipelines", p.Spec.UserRef.Name)
		assert.Equal(t, map[string]string{"team": "data"}, p.Spec.Tags)
		assert.Equal(t, "200m", ptr.To(p.Spec.Resources.Requests[corev1.ResourceCPU]).String())
		// CRD defaulting fills these; the API must still manage the Pipeline.
		assert.Equal(t, int32(1), ptr.Deref(p.Spec.Replicas, 0))
		assert.NotEmpty(t, p.Spec.UpdateStrategy)
	})

	t.Run("list includes pipelines from kubectl", func(t *testing.T) {
		resp, err := api.ListPipelines(ctx, connect.NewRequest(&dataplanev1.ListPipelinesRequest{}))
		require.NoError(t, err)
		var ids []string
		for _, p := range resp.Msg.GetPipelines() {
			ids = append(ids, p.GetId())
		}
		assert.ElementsMatch(t, []string{id, external.Name}, ids)

		resp, err = api.ListPipelines(ctx, connect.NewRequest(&dataplanev1.ListPipelinesRequest{
			Filter: &dataplanev1.ListPipelinesRequest_Filter{NameContains: "gitops"},
		}))
		require.NoError(t, err)
		require.Len(t, resp.Msg.GetPipelines(), 1)
		assert.Equal(t, external.Name, resp.Msg.GetPipelines()[0].GetId())
	})

	t.Run("update keeps fields the API doesn't own", func(t *testing.T) {
		// An admin raises replicas and sets a limit; Console edits the config.
		var p redpandav1alpha2.Pipeline
		require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: id}, &p))
		p.Spec.Replicas = ptr.To(int32(3))
		p.Spec.Resources.Limits = corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")}
		require.NoError(t, c.Update(ctx, &p))

		newConfig := "input:\n  generate:\n    mapping: root = \"bye\"\noutput:\n  drop: {}\n"
		_, err := api.UpdatePipeline(ctx, connect.NewRequest(&dataplanev1.UpdatePipelineRequest{
			Id:         id,
			Pipeline:   &dataplanev1.PipelineUpdate{ConfigYaml: newConfig},
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"config_yaml"}},
		}))
		require.NoError(t, err)

		require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: id}, &p))
		assert.Equal(t, newConfig, p.Spec.ConfigYAML)
		assert.Equal(t, "Orders to S3", p.Spec.DisplayName)
		assert.Equal(t, int32(3), ptr.Deref(p.Spec.Replicas, 0))
		assert.Equal(t, "1Gi", ptr.To(p.Spec.Resources.Limits[corev1.ResourceMemory]).String())
		assert.Equal(t, "200m", ptr.To(p.Spec.Resources.Requests[corev1.ResourceCPU]).String())
	})

	t.Run("stop and start", func(t *testing.T) {
		resp, err := api.StopPipeline(ctx, connect.NewRequest(&dataplanev1.StopPipelineRequest{Id: id}))
		require.NoError(t, err)
		assert.Equal(t, dataplanev1.Pipeline_STATE_STOPPING, resp.Msg.GetPipeline().GetState())
		var p redpandav1alpha2.Pipeline
		require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: id}, &p))
		assert.True(t, p.Spec.Paused)

		_, err = api.StartPipeline(ctx, connect.NewRequest(&dataplanev1.StartPipelineRequest{Id: id}))
		require.NoError(t, err)
		require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: id}, &p))
		assert.False(t, p.Spec.Paused)
	})

	t.Run("pipelines from kubectl are read-only", func(t *testing.T) {
		_, err := api.UpdatePipeline(ctx, connect.NewRequest(&dataplanev1.UpdatePipelineRequest{
			Id: external.Name, Pipeline: &dataplanev1.PipelineUpdate{ConfigYaml: validConfig},
		}))
		assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
		_, err = api.DeletePipeline(ctx, connect.NewRequest(&dataplanev1.DeletePipelineRequest{Id: external.Name}))
		assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
	})

	t.Run("labeled pipelines with unmanaged fields are read-only", func(t *testing.T) {
		extended := &redpandav1alpha2.Pipeline{
			ObjectMeta: metav1.ObjectMeta{Name: "extended", Namespace: testNamespace, Labels: map[string]string{managedByLabel: managedByValue}},
			Spec:       redpandav1alpha2.PipelineSpec{ConfigYAML: validConfig, Image: ptr.To("attacker/connect:latest")},
		}
		require.NoError(t, c.Create(ctx, extended))
		_, err := api.StopPipeline(ctx, connect.NewRequest(&dataplanev1.StopPipelineRequest{Id: extended.Name}))
		require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
		assert.Contains(t, err.Error(), "spec.image")
	})

	t.Run("config policy", func(t *testing.T) {
		_, err := api.CreatePipeline(ctx, connect.NewRequest(&dataplanev1.CreatePipelineRequest{Pipeline: &dataplanev1.PipelineCreate{
			ConfigYaml: "input:\n  subprocess:\n    name: cat\noutput:\n  drop: {}\n",
		}}))
		assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	})

	t.Run("delete", func(t *testing.T) {
		_, err := api.DeletePipeline(ctx, connect.NewRequest(&dataplanev1.DeletePipelineRequest{Id: id}))
		require.NoError(t, err)
		err = c.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: id}, &redpandav1alpha2.Pipeline{})
		assert.True(t, k8serrors.IsNotFound(err), "got %v", err)
	})

	t.Run("authentication", func(t *testing.T) {
		for name, tc := range map[string]struct {
			token string
			code  connect.Code
		}{
			"no token":       {token: "", code: connect.CodeUnauthenticated},
			"invalid token":  {token: "nope", code: connect.CodeUnauthenticated},
			"another caller": {token: otherToken, code: connect.CodePermissionDenied},
		} {
			t.Run(name, func(t *testing.T) {
				_, err := newProxyClient(srv.URL, tc.token, "mallory").ListPipelines(ctx, connect.NewRequest(&dataplanev1.ListPipelinesRequest{}))
				var cerr *connect.Error
				require.True(t, errors.As(err, &cerr), "got %v", err)
				assert.Equal(t, tc.code, cerr.Code())
			})
		}
	})

	t.Run("reviews are cached", func(t *testing.T) {
		before := reviewer.calls.Load()
		for range 5 {
			_, err := api.ListPipelines(ctx, connect.NewRequest(&dataplanev1.ListPipelinesRequest{}))
			require.NoError(t, err)
		}
		assert.Equal(t, before, reviewer.calls.Load(), "the console token was already reviewed")
	})
}

func TestNewServiceRequiresUserRefWithClusterRef(t *testing.T) {
	_, err := NewService(Config{Namespace: "ns", ClusterRef: "redpanda"}, nil, slog.New(slog.DiscardHandler))
	assert.ErrorContains(t, err, "userRef is required")
}

func TestNewAuthInterceptorFailsClosed(t *testing.T) {
	_, err := NewAuthInterceptor(AuthOptions{Reviewer: &staticReviewer{}})
	assert.Error(t, err, "no allowed callers")
	_, err = NewAuthInterceptor(AuthOptions{AllowUnauthenticated: true})
	assert.NoError(t, err)
}
