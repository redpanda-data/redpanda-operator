// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:namespace=console
package console

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cockroachdb/errors"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/yaml"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
)

//go:embed chart/values.yaml
var defaultValuesYAML []byte

// Scheme is a [runtime.Scheme] with the appropriate extensions to load all
// objects produced by the console chart.
var Scheme = runtime.NewScheme()

const (
	AppVersion           = "v3.3.1"
	ChartName            = "console"
	ConsoleContainerName = "console"
)

// +gotohelm:ignore=true
func init() {
	must(scheme.AddToScheme(Scheme))
}

// +gotohelm:ignore=true
func must(err error) {
	if err != nil {
		panic(err)
	}
}

type RenderState struct {
	ReleaseName  string
	Namespace    string
	Template     func(string) string
	CommonLabels map[string]string
	Values       RenderValues
}

// +gotohelm:ignore=true
func NewRenderState(namespace, name string, labels map[string]string, values PartialRenderValues) (*RenderState, error) {
	var rv RenderValues
	if err := yaml.Unmarshal(defaultValuesYAML, &rv); err != nil {
		return nil, errors.WithStack(err)
	}

	// Converting from PartialRenderValues to RenderValues is easiest to do
	// with JSON marshalling / unmarshalling as it lets us side step the type
	// system. Given that partial's serialize to fragments / partial,
	// unmarshaling into a full struct is nearly identical to helm's values
	// merging behavior.
	// Possible divergences may occur if slices are merged together but there's
	// no case for that with console's default values.
	out, err := json.Marshal(values)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	if err := json.Unmarshal(out, &rv); err != nil {
		return nil, errors.WithStack(err)
	}

	return &RenderState{
		Namespace:    namespace,
		ReleaseName:  name,
		Values:       rv,
		CommonLabels: labels,
		Template:     func(s string) string { return s },
	}, nil
}

// ChartName returns the name of this "chart", respecting any overrides.
//
// Previously known as "console.Name"
func (s *RenderState) ChartName() string {
	name := ChartName
	if s.Values.NameOverride != "" {
		name = s.Values.NameOverride
	}
	return cleanForK8s(name)
}

// FullName returns the fully qualified name of this installation, respecting
// any overrides. e.g. "release-console"
//
// Previously known as "console.Fullname"
func (s *RenderState) FullName() string {
	if s.Values.FullnameOverride != "" {
		return cleanForK8s(s.Values.FullnameOverride)
	}

	name := s.ChartName()

	if !strings.Contains(name, s.ReleaseName) {
		name = fmt.Sprintf("%s-%s", s.ReleaseName, name)
	}

	return cleanForK8s(name)
}

func (s *RenderState) SelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":     s.ChartName(),
		"app.kubernetes.io/instance": s.ReleaseName,
	}
}

// Labels returns labels updated with any chart or used imposed common labels.
// Keys in labels will be overridden if there is a conflict.
func (s *RenderState) Labels(labels map[string]string) map[string]string {
	if labels == nil {
		labels = map[string]string{}
	}

	for key, value := range s.SelectorLabels() {
		labels[key] = value
	}

	for key, value := range s.CommonLabels {
		labels[key] = value
	}

	for key, value := range s.Values.CommonLabels {
		labels[key] = value
	}

	return labels
}

// render is the entrypoint to both the go and helm versions of the console
// helm chart.
// In helm, _shims.render-manifest is used to call and filter the output of
// this function.
// In go, this function should be call by executing [ChartLabel.Render], which will
// handle construction of [helmette.Dot], subcharting, and output filtering.
func Render(state *RenderState) []kube.Object {
	manifests := []kube.Object{
		ServiceAccount(state),
		Secret(state),
		ConfigMap(state),
		Service(state),
		Ingress(state),
		Deployment(state),
		HorizontalPodAutoscaler(state),
	}

	// NB: This slice may contain nil interfaces!
	// Filtering happens elsewhere, don't call this function directly if you
	// can avoid it.
	return manifests
}

func Types() []kube.Object {
	return []kube.Object{
		&corev1.ServiceAccount{},
		&corev1.Secret{},
		&corev1.ConfigMap{},
		&corev1.Service{},
		&networkingv1.Ingress{},
		&appsv1.Deployment{},
		&autoscalingv2.HorizontalPodAutoscaler{},
	}
}

func cleanForK8s(s string) string {
	return helmette.TrimSuffix("-", helmette.Trunc(63, s))
}
