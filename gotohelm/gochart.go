// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package gotohelm

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"

	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/common-go/kube"
	"helm.sh/helm/v3/pkg/chart"
	"sigs.k8s.io/yaml"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
	"github.com/redpanda-data/redpanda-operator/pkg/helm"
)

type RenderFunc func(*helmette.Dot) []kube.Object

type GoChart struct {
	metadata      chart.Metadata
	defaultValues []byte
	renderFunc    RenderFunc
	dependencies  []Dependency
	fs            fs.FS
}

// MustLoad delegates to [Load] but panics upon any errors.
func MustLoad(f fs.FS, render RenderFunc, dependencies ...*GoChart) *GoChart {
	chart, err := Load(f, render, dependencies...)
	if err != nil {
		panic(err)
	}
	return chart
}

// Load hydrates a [GoChart] from helm YAML files and a top level [RenderFunc].
func Load(f fs.FS, render RenderFunc, subcharts ...*GoChart) (*GoChart, error) {
	chartYAML, err := fs.ReadFile(f, "Chart.yaml")
	if err != nil {
		return nil, err
	}

	defaultValuesYAML, err := fs.ReadFile(f, "values.yaml")
	if err != nil {
		return nil, err
	}

	var meta chart.Metadata
	if err := yaml.Unmarshal(chartYAML, &meta); err != nil {
		return nil, err
	}

	deps := make([]Dependency, len(meta.Dependencies))

	if len(meta.Dependencies) > 0 {
		// Only load Chart.lock if there are Dependencies as it won't exist otherwise.
		chartLockYAML, err := fs.ReadFile(f, "Chart.lock")
		if err != nil {
			return nil, err
		}

		var lock chart.Lock
		if err := yaml.Unmarshal(chartLockYAML, &lock); err != nil {
			return nil, err
		}

		if len(lock.Dependencies) != len(deps) {
			return nil, errors.Newf("Chart.lock dependencies != Chart.yaml dependencies. Did you forget to run helm dep update?")
		}

		if len(subcharts) != len(deps) {
			return nil, errors.Newf("Chart.yaml dependencies and provided subcharts don't match: %d != %d", len(subcharts), len(deps))
		}

		for i, chart := range subcharts {
			dep := meta.Dependencies[i]

			if chart.metadata.Name != dep.Name && chart.metadata.Name != dep.Alias {
				return nil, errors.Newf("invalid subchart ordering. Expected dependency at index %d to have name %q or %q got: %q", i, dep.Name, dep.Alias, chart.metadata.Name)
			}

			// Helm is SUPER finicky about .Name and .Version of subcharts. If
			// either is incorrect it'll return inane errors and have you
			// questioning your sanity. Given that .Name and .Version otherwise
			// controls how charts are published, it's quite possible that
			// Chart.yaml is not in sync with dependencies.
			// To prevent any issues down the line, shallow copy the chart
			// (which also clones metadata) and set the version and name
			// manually.
			// This might bite us in the but most instances of gotohelm have
			// tests to assert that helm and go behavior are equivalent which
			// should save us most of the time.
			chart := *chart
			chart.metadata.Name = meta.Dependencies[i].Name
			chart.metadata.Version = lock.Dependencies[i].Version

			deps[i] = Dependency{
				GoChart: &chart,
				HelmDep: meta.Dependencies[i],
				Lock:    lock.Dependencies[i],
			}
		}
	}

	return &GoChart{
		metadata:      meta,
		defaultValues: defaultValuesYAML,
		renderFunc:    render,
		dependencies:  deps,
		fs:            f,
	}, nil
}

// WriteArchive is the equivalent of `helm package` executed on a [GoChart].
// Provided that the GoChart's FS is correctly configured, the outputs will
// be equivalent.
func (c *GoChart) WriteArchive(w io.Writer) error {
	// Ideally, this would all be done in memory but it's surprisingly
	// difficult to stitch together different fs.FS's. So we just dump it to
	// disk.
	dir, err := os.MkdirTemp(os.TempDir(), "gotohelm-packaging")
	if err != nil {
		return errors.WithStack(err)
	}

	defer os.RemoveAll(dir)

	if err := c.Write(dir); err != nil {
		return err
	}

	gzipWriter := gzip.NewWriter(w)
	defer gzipWriter.Close()

	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	if err := tarWriter.AddFS(os.DirFS(dir)); err != nil {
		return errors.WithStack(err)
	}

	return nil
}

// Write writes this chart to dir in a format compatible with the helm CLI
// tool.
// NOTE: Write relies on gotohelm having been run ahead of GoChart consumption
// as it just writes out the embedded FS.
func (c *GoChart) Write(dir string) error {
	// Chart archives are nested under their name.
	dir = filepath.Join(dir, c.metadata.Name)

	if err := os.CopyFS(dir, c.fs); err != nil {
		return errors.WithStack(err)
	}

	// helm package strips comments and reformats Chart.yaml. We do the same to
	// ensure that WriteArchive produces the same output as helm package.
	chartYAML, err := yaml.Marshal(c.metadata)
	if err != nil {
		return errors.WithStack(err)
	}

	// NB: 0o666 is taken from .CopyFS.
	//nolint:gosec // Primarily used in tests, these permission are not security critical.
	if err := os.WriteFile(filepath.Join(dir, "Chart.yaml"), chartYAML, 0o666); err != nil {
		return errors.WithStack(err)
	}

	depDir := filepath.Join(dir, "charts")

	// NB: 0o777 is taken from .CopyFS.
	//nolint:gosec // Primarily used in tests, these permission are not security critical.
	if err := os.Mkdir(depDir, 0o777); err != nil {
		return errors.WithStack(err)
	}

	for _, dep := range c.dependencies {
		if err := dep.GoChart.Write(depDir); err != nil {
			return errors.WithStack(err)
		}
	}

	return nil
}

// LoadValues coheres the provided values into a [helmette.Values] and merges
// it with the default values of this chart. Dependencies are not loaded.
func (c *GoChart) LoadValues(values any) (helmette.Values, error) {
	valuesYaml, err := yaml.Marshal(values)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	merged, err := helm.MergeYAMLValues(c.defaultValues, valuesYaml)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return merged, nil
}

// Dot constructs a [helmette.Dot] for this chart and any dependencies it has,
// taking into consideration the dependencies' condition.
func (c *GoChart) Dot(cfg *kube.RESTConfig, release helmette.Release, values any) (*helmette.Dot, error) {
	subcharts := map[string]*helmette.Dot{}

	parentValues, err := c.LoadValues(values)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	// Respect/support for helm's special "global" stanza.
	// NB: global's are injected to subcharts regardless of whether or not it's
	// present in the parent. IT IS NOT injected in the parent.
	globals, ok := parentValues["global"]
	if !ok {
		globals = struct{}{}
	}

	// Helm's behavior here is mind boggling but it's most simply represented as:
	// 1. Load `subchart`'s values with it's stanza from the parent's values
	// 2. Inject subchart's loaded values back into it's stanza in the parent's values
	// 3. Evaluate `enabled` on the now merged parent values
	// 4. If 4 evaluated to false, restore the subchart stanza to it's original value.
	for _, dep := range c.dependencies {
		depValues, err := parentValues.Table(dep.Key())
		if err != nil {
			return nil, errors.WithStack(err)
		}

		// 1. Load up the subchart's values with our subchart's stanza from the
		// parent as a base.
		subchartDot, err := dep.GoChart.Dot(cfg, release, depValues)
		if err != nil {
			return nil, errors.WithStack(err)
		}

		// Inject globals into the merged .Values of the subchart.
		subchartDot.Values["global"] = globals

		// If a dependency has an alias assigned to it, it's Chart.Name get's
		// set to the value of the alias.
		subchartDot.Chart.Name = dep.Key()

		// 2. Inject the loaded values into parent's values.
		parentValues[dep.Key()] = subchartDot.Values

		// 3. Evaluate `enabled` on the merged values.
		enabled, err := dep.Enabled(parentValues)
		if err != nil {
			return nil, errors.WithStack(err)
		}

		// 4. If our dependency is enabled, register it in subcharts.
		// Otherwise, revert the stanza back to its original value.
		if enabled {
			subcharts[dep.Key()] = subchartDot
		} else {
			parentValues[dep.Key()] = depValues
		}
	}

	templates, err := fs.Sub(c.fs, "templates")
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return &helmette.Dot{
		KubeConfig: cfg,
		Release:    release,
		Subcharts:  subcharts,
		Values:     parentValues,
		Templates:  templates,
		Files:      helmette.NewFiles(c.fs),
		Chart: helmette.Chart{
			Name:       c.metadata.Name,
			Version:    c.metadata.Version,
			AppVersion: c.metadata.AppVersion,
		},
	}, nil
}

// Render is the golang equivalent of invoking `helm template/install/upgrade`
// with the exception of excluding NOTES.txt.
//
// Helm hooks are included in the returned slice, it's up to the caller
// to filter them.
//
// If cfg is null, the chart will be rendered "offline" causing functions like
// [helmette.Lookup] to always return false.
func (c *GoChart) Render(cfg *kube.RESTConfig, release helmette.Release, values any) ([]kube.Object, error) {
	dot, err := c.Dot(cfg, release, values)
	if err != nil {
		return nil, err
	}

	return c.render(dot)
}

// Metadata returns the parsed [chart.Metadata] describing this chart.
func (c *GoChart) Metadata() chart.Metadata {
	return c.metadata
}

// doRender is a helper to catch any panics from renderFunc and convert them to
// errors.
func (c *GoChart) doRender(dot *helmette.Dot) (_ []kube.Object, err error) {
	defer func() {
		switch r := recover().(type) {
		case nil:
		case error:
			err = errors.Wrapf(r, "chart execution failed")
		default:
			err = errors.Newf("chart execution failed: %#v", r)
		}
	}()

	manifests := c.renderFunc(dot)

	// renderFunc is expected to return nil interfaces.
	// In the helm world, these nils are filtered out by
	// _shims.render-manifests.
	j := 0
	for i := range manifests {
		// Handle the nil unboxing issue.
		if reflect.ValueOf(manifests[i]).IsNil() {
			continue
		}
		manifests[j] = manifests[i]
		j++
	}

	return manifests[:j], nil
}

func (c *GoChart) render(dot *helmette.Dot) ([]kube.Object, error) {
	manifests, err := c.doRender(dot)
	if err != nil {
		return nil, err
	}

	// Loop over dependencies to preserve ordering
	for _, dep := range c.dependencies {
		// If a dep isn't present in Subcharts, that means it wasn't enabled.
		depDot, ok := dot.Subcharts[dep.Key()]
		if !ok {
			continue
		}

		subchartManifests, err := dep.GoChart.render(depDot)
		if err != nil {
			return nil, err
		}

		manifests = append(manifests, subchartManifests...)
	}

	return manifests, nil
}

type Dependency struct {
	GoChart *GoChart
	HelmDep *chart.Dependency
	Lock    *chart.Dependency
}

func (d *Dependency) Key() string {
	if d.HelmDep.Alias != "" {
		return d.HelmDep.Alias
	}
	return d.HelmDep.Name
}

func (d *Dependency) Enabled(merged helmette.Values) (bool, error) {
	// https://github.com/helm/helm/blob/145d12f82fc7a2e39a17713340825686b661e0a1/pkg/chartutil/dependencies.go#L48
	if d.HelmDep.Condition == "" {
		return true, nil
	}

	enabled, err := merged.PathValue(d.HelmDep.Condition)
	if err != nil {
		return false, errors.WithStack(err)
	}

	asBool, ok := enabled.(bool)
	if !ok {
		return false, errors.Newf("evaluating subchart %q condition %q, expected %t; got: %t (%v)", d.Key(), d.HelmDep.Condition, true, enabled, enabled)
	}

	return asBool, nil
}
