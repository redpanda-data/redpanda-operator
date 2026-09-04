// Package supported is the negative control: everything in it transpiles, so
// the analyzer must stay silent. It's a condensed version of the constructs
// that gotohelm's own testdata charts rely on.
package supported

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

type Values struct {
	metav1.TypeMeta `json:",inline"`

	Name     string            `json:"name"`
	Replicas int32             `json:"replicas"`
	Labels   map[string]string `json:"labels,omitempty"`
	Ports    []int32           `json:"ports,omitempty"`
	Enabled  *bool             `json:"enabled,omitempty"`
	Requests resource.Quantity `json:"requests"`
}

func (v Values) FullName() string {
	return fmt.Sprintf("%s-%s", v.Name, "suffix")
}

func Service(dot *helmette.Dot) *corev1.Service {
	values := helmette.Unwrap[Values](dot.Values)

	var ports []corev1.ServicePort
	for _, port := range values.Ports {
		ports = append(ports, corev1.ServicePort{
			Name: fmt.Sprintf("port-%d", port),
			Port: port,
		})
	}

	labels := map[string]string{}
	for key, value := range helmette.SortedMap(values.Labels) {
		labels[key] = strings.ToLower(value)
	}

	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      values.FullName(),
			Namespace: dot.Release.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Ports: ports,
		},
	}
}

func Arithmetic(values Values) int32 {
	total := int32(0)
	for i := int32(0); i < values.Replicas; i++ {
		total = total + i
	}

	if ptr.Deref(values.Enabled, false) {
		total = total * 2
	}

	return total
}

func Conditionals(values Values) string {
	if value, ok := values.Labels["key"]; ok {
		return value
	} else if values.Name != "" {
		return values.Name
	}

	return "default"
}

func Assertions(value any) string {
	if s, ok := value.(string); ok {
		return s
	}

	if i, ok := helmette.AsIntegral[int](value); ok {
		return fmt.Sprintf("%d", i)
	}

	return ""
}

func Slicing(xs []string, s string) ([]string, string) {
	return xs[1:], s[1:2]
}

func Lookups(dot *helmette.Dot) string {
	svc, found := helmette.Lookup[corev1.Service](dot, "namespace", "name")
	if !found {
		return ""
	}
	// NB: gotohelm does not flatten named embedded structs, so the promoted
	// field has to be reached through ObjectMeta.
	return svc.ObjectMeta.Name
}

func Builtins(xs []string, m map[string]string) int {
	xs = append(xs, "a", "b")
	xs = append(xs, xs...)
	delete(m, "key")
	return len(xs) + len(m)
}

// Parallel assignment, including the forms a naive unroll would get wrong.
// rewrite.UnrollAssignments captures the values first, so these transpile.
func ParallelAssignment(a, b string) []string {
	a, b = b, a

	c, d := 1, 2
	c, d = d, c

	return []string{a, b, string(rune(c)), string(rune(d))}
}
