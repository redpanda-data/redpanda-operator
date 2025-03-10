// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package helmette

import (
	"context"
	"io/fs"
	"maps"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig/v3"
	"github.com/cockroachdb/errors"
	"helm.sh/helm/v3/pkg/chartutil"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/redpanda-data/redpanda-operator/pkg/kube"
)

// Dot is a representation of the "global" context or `.` in the execution
// of a helm template.
// See also: https://github.com/helm/helm/blob/3764b483b385a12e7d3765bff38eced840362049/pkg/chartutil/values.go#L137-L166
type Dot struct {
	Values    Values
	Release   Release
	Chart     Chart
	Subcharts map[string]*Dot
	// Capabilities

	// KubeConfig is a hacked in value to allow `Lookup` to not rely on global
	// values.
	// It is a pointer to explicitly allow null values, similar to how `helm
	// template` provides a mock Kubernetes client.
	// WARNING: DO NOT USE OR REFERENCE IN HELM CHARTS. IT WILL NOT WORK.
	KubeConfig *kube.RESTConfig

	// Templates, similar to KubeConfig, is a hacked in value to support `tpl`.
	// It is an FS containing the contents of the charts template/ directory.
	// As fs.FS is not trivial to serialize, the `go run` test runner has a
	// special case to rehydrate it.
	// WARNING: DO NOT USE OR REFERENCE IN HELM CHARTS. IT WILL NOT WORK.
	Templates fs.FS `json:"-"`
}

type Release struct {
	Name      string
	Namespace string
	Service   string
	IsUpgrade bool
	IsInstall bool
	// Revision
}

type Chart struct {
	Name       string
	Version    string
	AppVersion string
}

type Values = chartutil.Values

// https://helm.sh/docs/howto/charts_tips_and_tricks/#using-the-tpl-function
func Tpl(dot *Dot, tpl string, context any) string {
	// gotohelm's tpl implementation is intentionally more restrictive than
	// helm's. Anything that accesses this function should be a short and
	// simple user provided template.
	const recursionLimit = 10
	recusion := 0
	maybeRecurse := func() error {
		recusion++
		if recusion > recursionLimit {
			return errors.New("recursion limit reached")
		}
		return nil
	}

	fns := sprig.TxtFuncMap()
	maps.Copy(fns, template.FuncMap{
		"toYaml":   ToYaml,
		"fromYaml": FromYaml,
		"toJson":   ToJSON,
		"fromJson": FromJSON,

		// Any helm function needs to be stubbed out to ensure that parse works
		// as function calls are checked at parse time and we have to parse the
		// entire chart. Again, we're intentionally restrictive here.
		"lookup":        func(...any) error { return errors.New("not implemented") },
		"toToml":        func(...any) error { return errors.New("not implemented") },
		"fromYamlArray": func(...any) error { return errors.New("not implemented") },
		"fromJsonArray": func(...any) error { return errors.New("not implemented") },

		// These need to be lazily bound as they refer to the template instance they're bound to.
		"include": func(string, any) (string, error) { return "", errors.New("not implemented") },
		"tpl":     func(string, any) (string, error) { return "", errors.New("not implemented") },
	})

	// [template.ParseFS] will return an error if any of the provided globs
	// turn up zero results. As this is a possible and valid case for us, we
	// loop over our patterns and filter out the error if encountered.
	tmpl := template.New("").Funcs(fns)
	patterns := []string{"*.tpl", "*.yaml"}
	for _, pattern := range patterns {
		t, err := tmpl.ParseFS(dot.Templates, pattern)
		if err != nil {
			if strings.HasPrefix(err.Error(), "template: pattern matches no files:") {
				continue
			}
			panic(err)
		}
		tmpl = t
	}

	tmpl = template.Must(tmpl.Parse(tpl))

	tmpl.Funcs(template.FuncMap{
		"include": func(name string, data any) (string, error) {
			if err := maybeRecurse(); err != nil {
				return "", err
			}

			var b strings.Builder
			if err := tmpl.ExecuteTemplate(&b, name, data); err != nil {
				return "", err
			}
			return b.String(), nil
		},
		"tpl": func(text string, data any) (string, error) {
			if err := maybeRecurse(); err != nil {
				return "", err
			}

			t, err := tmpl.Clone()
			if err != nil {
				return "", err
			}

			t, err = t.Parse(text)
			if err != nil {
				return "", err
			}

			var b strings.Builder
			if err := t.Execute(&b, data); err != nil {
				return "", err
			}
			return b.String(), nil
		},
	})

	var b strings.Builder
	if err := tmpl.Execute(&b, context); err != nil {
		panic(err)
	}
	return b.String()
}

// Lookup is a wrapper around helm's builtin lookup function that instead
// returns `nil, false` if the lookup fails instead of an empty dictionary.
// See: https://github.com/helm/helm/blob/e24e31f6cc122405ae25069f5b3960036c202c46/pkg/engine/lookup_func.go#L60-L97
func Lookup[T any, PT kube.AddrofObject[T]](dot *Dot, namespace, name string) (obj *T, found bool) {
	obj, found, err := SafeLookup[T, PT](dot, namespace, name)
	if err != nil {
		panic(err)
	}

	return obj, found
}

// SafeLookup is a wrapper around helm's builtin lookup function. It acts
// exactly like Lookup except it returns any errors that may have occurred
// in the underlying lookup operations.
func SafeLookup[T any, PT kube.AddrofObject[T]](dot *Dot, namespace, name string) (*T, bool, error) {
	// Special case, if no KubeConfig has been provided, short circuit and
	// report that the object wasn't found. This allows execution without
	// access to a Kubernetes cluster, like `helm template`.
	// https://github.com/helm/helm/blob/32530594381efc783e17f6fdd3a4a936548bfd04/pkg/action/install.go#L274-L275
	if dot.KubeConfig == nil {
		return nil, false, nil
	}

	ctl, err := kube.FromRESTConfig(dot.KubeConfig)
	if err != nil {
		return nil, false, err
	}

	obj, err := kube.Get[T, PT](context.Background(), ctl, kube.ObjectKey{Namespace: namespace, Name: name})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, false, err
		}

		return nil, false, nil
	}

	return obj, true, nil
}
