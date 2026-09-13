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
	"slices"
	"sync"

	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/common-go/kube"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chartutil"
	"sigs.k8s.io/yaml"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

type RenderFunc func(*helmette.Dot) []kube.Object

// capabilitiesCache holds lazily resolved Kubernetes capabilities. It is
// allocated once per top-level chart and shared (via pointer) with shallow
// copies created for subcharts in Load, avoiding a copied-lock bug.
type capabilitiesCache struct {
	// mu guards cached and capabilities. We use a mutex rather than sync.Once
	// so that a transient failure (e.g. Dot() called with an empty rest.Config)
	// does not permanently cache the error.
	mu     sync.Mutex
	cached bool
	caps   helmette.Capabilities
}

type GoChart struct {
	kubeversion   *helmette.KubeVersion
	metadata      chart.Metadata
	defaultValues []byte
	renderFunc    RenderFunc
	dependencies  []Dependency
	fs            fs.FS

	capCache *capabilitiesCache
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
		capCache:      &capabilitiesCache{},
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

// LoadValues coheres the provided values into a [helmette.Values] with helm's
// own values pipeline. It merges in the chart's default values, processes
// dependencies, and "coalesces" the output.
//
// Coalescing is NOT an idempotent process. The output of [LoadValues] should
// not be passed to other GoChart functions. Doing so will cause divergence
// from helm's behavior and significantly increase the chances acquiring coal
// on December 25th.
func (c *GoChart) LoadValues(values any) (helmette.Values, error) {
	_, merged, err := c.coalesce(values)
	return merged, err
}

// coalesce hydrates the [chart.Chart] tree equivalent of this GoChart and runs
// helm's values pipeline over the provided values, returning both. Subcharts
// disabled by a condition or tag are pruned from the returned tree.
//
// To "coalesce" values helm walks the charts defaults. If any user provided
// values at those paths are `null`, they are pruned. User provided nulls at
// paths not in the chart defaults as preserved.
func (c *GoChart) coalesce(values any) (*chart.Chart, helmette.Values, error) {
	valuesYAML, err := yaml.Marshal(values)
	if err != nil {
		return nil, nil, errors.WithStack(err)
	}

	var provided map[string]any
	if err := yaml.Unmarshal(valuesYAML, &provided); err != nil {
		return nil, nil, errors.WithStack(err)
	}

	hc, err := c.helmChart()
	if err != nil {
		return nil, nil, err
	}

	// Order matters and mirrors `helm install`: resolving conditions first
	// prunes disabled subcharts from the tree, which is what leaves their
	// stanza holding only the caller's values instead of the subchart's
	// defaults.
	// https://github.com/helm/helm/blob/v3.18.5/pkg/action/install.go#L249-L304
	if err := chartutil.ProcessDependenciesWithMerge(hc, provided); err != nil {
		return nil, nil, errors.WithStack(err)
	}

	merged, err := chartutil.CoalesceValues(hc, provided)
	if err != nil {
		return nil, nil, errors.WithStack(err)
	}

	return hc, merged, nil
}

// helmChart hydrates the [chart.Chart] tree equivalent of this GoChart and its
// dependencies, transitively. A fresh tree is returned on every call as helm's
// dependency processing mutates all of it in place.
func (c *GoChart) helmChart() (*chart.Chart, error) {
	var values map[string]any
	if err := yaml.Unmarshal(c.defaultValues, &values); err != nil {
		return nil, errors.WithStack(err)
	}

	// NB: Clone preserves nil-ness, which matters as
	// chartutil.processDependencyEnabled short circuits a LOT of work on
	// .Dependencies == nil.
	metadata := c.metadata
	metadata.Dependencies = slices.Clone(c.metadata.Dependencies)
	for i, dep := range metadata.Dependencies {
		cloned := *dep
		metadata.Dependencies[i] = &cloned
	}

	hc := &chart.Chart{Metadata: &metadata, Values: values}

	deps := make([]*chart.Chart, 0, len(c.dependencies))
	for _, dep := range c.dependencies {
		subchart, err := dep.GoChart.helmChart()
		if err != nil {
			return nil, err
		}
		deps = append(deps, subchart)
	}
	hc.SetDependencies(deps...)

	return hc, nil
}

// WithSyntheticKubeVersion allows a caller to override the KubeVersion passed
// off to the underlying go-rendered chart.
func (c *GoChart) WithSyntheticKubeVersion(version *helmette.KubeVersion) *GoChart {
	return &GoChart{
		kubeversion:   version,
		metadata:      c.metadata,
		defaultValues: c.defaultValues,
		renderFunc:    c.renderFunc,
		dependencies:  c.dependencies,
		fs:            c.fs,
		capCache:      c.capCache,
	}
}

// resolveCapabilities returns cached capabilities if available, otherwise
// queries the Kubernetes API server and caches the result on success. Failures
// are not cached so that a transient error (e.g. an empty rest.Config from a
// sidecar context) does not permanently poison the cache for the main
// reconciler. If resolution fails, empty capabilities are returned because
// capabilities are only used for non-critical metadata (e.g. metrics env vars)
// and should not block reconciliation.
//
// A nil cfg is likewise never cached: NewCapabilities(nil) returns an empty
// stub WITHOUT an error, and charts are commonly package-global — if a
// nil-config caller (e.g. a controller that renders without cluster access)
// wins the first-render race at process start, caching its stub would pin
// EMPTY capabilities for every later real-config render in the process.
// The symptom: capability-derived pod-template fields (the
// REDPANDA_METRICS_K8S_VERSION env) render empty after an operator
// restart, changing the StatefulSet revision and triggering a spurious
// cluster-wide rolling restart.
func (c *GoChart) resolveCapabilities(cfg *kube.RESTConfig) (helmette.Capabilities, error) {
	c.capCache.mu.Lock()
	defer c.capCache.mu.Unlock()

	if c.capCache.cached {
		return c.capCache.caps, nil
	}

	caps, err := helmette.NewCapabilities(cfg)
	if cfg == nil {
		// A nil config legitimately renders without cluster access (e.g. the
		// sidecar, or a controller rendering offline): NewCapabilities(nil)
		// returns an empty stub with no error. Never cached (see above), never
		// an error.
		return caps, nil
	}
	if err != nil {
		// A REAL config that failed discovery must NOT render with empty
		// capabilities. The capability-derived KubeVersion feeds the broker
		// StatefulSet pod template (REDPANDA_METRICS_K8S_VERSION), so an empty
		// value changes the pod-template revision and triggers a spurious
		// cluster-wide rolling restart. Surface the error so the caller
		// requeues and retries against a resolvable apiserver, rather than
		// silently rendering — and applying — a rolled template. Uncached: the
		// next attempt re-resolves.
		return helmette.Capabilities{}, errors.WithStack(err)
	}

	c.capCache.caps = caps
	c.capCache.cached = true
	return caps, nil
}

// Dot constructs a [helmette.Dot] for this chart and any dependencies it has,
// taking into consideration the dependencies' conditions and tags. `values`
// should be raw user values. DO NOT pass the results of [GoChart.LoadValues]
// in.
func (c *GoChart) Dot(cfg *kube.RESTConfig, release helmette.Release, values any) (*helmette.Dot, error) {
	hc, merged, err := c.coalesce(values)
	if err != nil {
		return nil, err
	}

	return c.dot(cfg, release, hc, merged)
}

// dot builds the [helmette.Dot] for hc, the [chart.Chart] equivalent of c, from
// an already coalesced set of values. It is helm's engine.recAllTpls.
// https://github.com/helm/helm/blob/v3.18.5/pkg/engine/engine.go#L389-L423
func (c *GoChart) dot(cfg *kube.RESTConfig, release helmette.Release, hc *chart.Chart, values helmette.Values) (*helmette.Dot, error) {
	// Keyed by Dependency.Key, which is the alias of an aliased dependency --
	// exactly what ProcessDependencies renames the subchart to.
	goCharts := make(map[string]*GoChart, len(c.dependencies))
	for _, dep := range c.dependencies {
		goCharts[dep.Key()] = dep.GoChart
	}

	subcharts := map[string]*helmette.Dot{}

	// NB: hc.Dependencies() holds only the subcharts that survived
	// ProcessDependencies, which is the set helm's engine renders.
	for _, subchart := range hc.Dependencies() {
		goChart, ok := goCharts[subchart.Name()]
		if !ok {
			return nil, errors.Newf("no GoChart registered for subchart %q of %q", subchart.Name(), hc.Name())
		}

		// CoalesceValues has already placed the subchart's values, helm's
		// special "global" stanza included, into the parent's.
		subchartValues, err := values.Table(subchart.Name())
		if err != nil {
			return nil, errors.WithStack(err)
		}

		// Propagate any synthetic KubeVersion override so the subchart sees the
		// same capabilities as the parent.
		if c.kubeversion != nil {
			goChart = goChart.WithSyntheticKubeVersion(c.kubeversion)
		}

		subchartDot, err := goChart.dot(cfg, release, subchart, subchartValues)
		if err != nil {
			return nil, err
		}

		subcharts[subchart.Name()] = subchartDot
	}

	templates, err := fs.Sub(c.fs, "templates")
	if err != nil {
		return nil, errors.WithStack(err)
	}

	capabilities, err := c.resolveCapabilities(cfg)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	if c.kubeversion != nil {
		capabilities.KubeVersion = *c.kubeversion
	}

	return &helmette.Dot{
		KubeConfig:   cfg,
		Release:      release,
		Subcharts:    subcharts,
		Values:       values,
		Templates:    templates,
		Files:        helmette.NewFiles(c.fs),
		Capabilities: capabilities,
		Chart: helmette.Chart{
			// NB: hc.Metadata, not c.metadata. helm renames aliased subcharts
			// to their alias.
			Name:       hc.Metadata.Name,
			Version:    hc.Metadata.Version,
			AppVersion: hc.Metadata.AppVersion,
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
