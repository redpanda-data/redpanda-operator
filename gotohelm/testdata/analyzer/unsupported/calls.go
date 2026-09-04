package unsupported

import (
	"errors"
	"slices"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

// Widget stands in for a custom resource: a well formed Kubernetes object
// that isn't registered in client-go's scheme.
type Widget struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Name string `json:"name"`
}

func (w *Widget) DeepCopyObject() runtime.Object { return &Widget{} }

func (w Widget) Describe() string {
	return w.Name
}

func SupportedCall(s string, xs []string) string {
	return helmette.Quote(strings.Join(xs, s))
}

func SupportedMethod(w Widget) string {
	return w.Describe()
}

func StdlibCall(i int) string {
	return strconv.Itoa(i) // want `strconv.Itoa cannot be called from a chart`
}

func ErrorConstruction() error {
	return errors.New("boom") // want `errors.New cannot be called from a chart`
}

func ErrorMethod(err error) string {
	return err.Error() // want `Error is declared in the universe scope`
}

func MethodValue(w Widget) string {
	// Method values are supported; they transpile into a template name paired
	// with a deep copy of the receiver.
	describe := w.Describe
	return describe()
}

func MethodExpression(w Widget) string {
	return Widget.Describe(w) // want `method expressions are not supported`
}

func SortedInts(xs []int) []int {
	return slices.Sorted(slices.Values(xs)) // want `slices.Sorted is only supported for strings` `slices.Values cannot be called from a chart`
}

func SortedStrings(xs []string) []string {
	return slices.Sorted(slices.Values(xs)) // want `slices.Values cannot be called from a chart`
}

func LookupService(dot *helmette.Dot) *corev1.Service {
	svc, _ := helmette.Lookup[corev1.Service](dot, "namespace", "name")
	return svc
}

func LookupCustomResource(dot *helmette.Dot) *Widget {
	widget, _ := helmette.Lookup[Widget](dot, "namespace", "name") // want `is not registered in client-go's scheme`
	return widget
}

// InChartReference is fine; Describe transpiles alongside its caller.
func InChartReference(w Widget) func() string {
	return w.Describe
}

func StdlibReference() func(int) string {
	return strconv.Itoa // want `strconv.Itoa is not in gotohelm's dependency list`
}

func InChartFunctionReference() func(string) string {
	return Supported2
}

func Supported2(s string) string { return s }

func Nullary() func() int { return nil }

func CallOfACallResult() int {
	return Nullary()() // want `gotohelm can only call named functions and methods`
}

// A cross package constant is inlined and is fine.
func CrossPackageConstant() string {
	return corev1.NamespaceAll
}

func CrossPackageVariable() string {
	return metav1.Unversioned.Version // want `the package level variable metav1.Unversioned is not supported`
}
