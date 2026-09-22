// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package endpointsteering

import (
	"context"
	"testing"
	"time"

	"github.com/redpanda-data/common-go/portmapper"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMapperConfig(t *testing.T) {
	_, err := mapperConfig(Options{})
	require.Error(t, err, "a resolver is what makes the checker able to recognise brokers at all")

	_, err = mapperConfig(Options{Resolver: Resolvers{}})
	require.Error(t, err, "an empty resolver set would drain every opted-in Service")

	resolver := ResolverFunc(func(context.Context, string, string) (Cluster, error) {
		return nil, ErrUnknownCluster
	})
	cfg, err := mapperConfig(Options{Resolver: Resolvers{resolver}, ClusterDomain: "cluster.local"})
	require.NoError(t, err)
	require.Equal(t, ManagedBy, cfg.ManagedBy)
	require.Equal(t, portmapper.AnnotationKey(ServiceAnnotation), cfg.ServiceKey)
	require.Equal(t, portmapper.LabelKey(PodGroupLabel), cfg.PodKey)
	require.Equal(t, DefaultResyncPeriod, cfg.ResyncPeriod)
	require.NotNil(t, cfg.Membership)

	// The library validates keys and the managed-by value; whatever we hand
	// it has to pass.
	_, err = portmapper.New(cfg)
	require.NoError(t, err)

	cfg, err = mapperConfig(Options{Resolver: resolver, ResyncPeriod: time.Minute})
	require.NoError(t, err)
	require.Equal(t, time.Minute, cfg.ResyncPeriod)
}

// TestSteer pins the shape steering gives a Service: no selector, so the
// native EndpointSlice controller leaves it alone, and the annotation naming
// the cluster for this one.
func TestSteer(t *testing.T) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				"example.com/keep": "me",
				// A user-supplied value for the annotation is overridden:
				// the controller needs it to name the cluster.
				ServiceAnnotation: "someone-else",
			},
		},
		Spec: corev1.ServiceSpec{
			Selector:                 map[string]string{"app.kubernetes.io/instance": "rp"},
			PublishNotReadyAddresses: true,
		},
	}

	Steer(svc, "rp")

	require.Nil(t, svc.Spec.Selector)
	require.Equal(t, "rp", svc.Annotations[ServiceAnnotation])
	require.Equal(t, "me", svc.Annotations["example.com/keep"])
	require.True(t, svc.Spec.PublishNotReadyAddresses, "broker discovery still needs not-ready addresses")

	// A Service with no annotations at all is the common case for one the
	// operator renders.
	bare := &corev1.Service{Spec: corev1.ServiceSpec{Selector: map[string]string{"a": "b"}}}
	Steer(bare, "rp")
	require.Nil(t, bare.Spec.Selector)
	require.Equal(t, map[string]string{ServiceAnnotation: "rp"}, bare.Annotations)
}
