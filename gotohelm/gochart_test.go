// Copyright 2025 Redpanda Data, Inc.
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
	"bytes"
	"compress/gzip"
	"context"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/gonvenience/ytbx"
	"github.com/homeport/dyff/pkg/dyff"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yamlv3 "gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
	"github.com/redpanda-data/redpanda-operator/pkg/helm"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

//go:embed testdata/subchart/*/*.yaml
//go:embed testdata/subchart/*/*.lock
//go:embed testdata/subchart/*/templates
var subchartsFS embed.FS

var getTestCharts = sync.OnceValue(func() []*GoChart {
	// Build deps of all our charts before loading them.
	for _, chartPath := range []string{
		"./testdata/subchart/dependency",
		"./testdata/subchart/dependency-excluded-by-default",
		"./testdata/subchart/dependency-included-by-default",
		"./testdata/subchart/values-overwrite",
		"./testdata/subchart/root",
	} {
		// Update Chart.lock
		if _, err := exec.Command("helm", "dep", "update", chartPath, "--skip-refresh").CombinedOutput(); err != nil {
			panic(err)
		}

		// Update charts/
		if _, err := exec.Command("helm", "dep", "build", chartPath, "--skip-refresh").CombinedOutput(); err != nil {
			panic(err)
		}
	}

	subFS := func(dir string) fs.FS {
		sub, err := fs.Sub(subchartsFS, "testdata/subchart/"+dir)
		if err != nil {
			panic(err)
		}
		return sub
	}

	// The chart dependency graph
	//            ┌───────────────┐   ┌──────────┐
	//     ┌─────►│valuesOverwrite├──►│Dependency│
	//     │      └───────────────┘   └──────────┘
	//     │
	// ┌───┴┐     ┌──────────┐        ┌──────────┐
	// │root┼────►│ExcludeDep├───────►│Dependency│
	// └───┬┘     └──────────┘        └──────────┘
	//     │
	//     │      ┌──────────┐        ┌───────────┐
	//     └─────►│IncludeDep├───────►│Dependency │
	//            └──────────┘        └───────────┘
	// Graph created by https://asciiflow.com/#/
	//
	// ExcludeDep - has condition that points to value which is false.
	// IncludeDep - has condition that points to value which is true.
	// valuesOverwrite - does not have any condition
	//
	// All charts are creating Config map which has one data, the rendered values
	dep := MustLoad(subFS("dependency"), renderConfigMap)
	valuesOverwrite := MustLoad(subFS("values-overwrite"), renderConfigMap, dep)
	excludeDep := MustLoad(subFS("dependency-excluded-by-default"), renderConfigMap, dep)
	includeDep := MustLoad(subFS("dependency-included-by-default"), renderConfigMap, dep)
	root := MustLoad(subFS("root"), renderConfigMap, valuesOverwrite, includeDep, excludeDep)

	return []*GoChart{
		root,
		includeDep,
		excludeDep,
		valuesOverwrite,
		dep,
	}
})

func TestDependencyChainRender(t *testing.T) {
	charts := getTestCharts()
	root := charts[0]

	helmCli, err := helm.New(helm.Options{ConfigHome: testutil.TempDir(t)})
	require.NoError(t, err)

	inputVal, err := os.ReadFile("testdata/subchart/root/input-val.yaml")
	require.NoError(t, err)

	inputValues := map[string]any{}

	err = yaml.Unmarshal(inputVal, &inputValues)
	require.NoError(t, err)

	expected, err := helmCli.Template(context.Background(), "testdata/subchart/root", helm.TemplateOptions{
		Name:      "subchart",
		Namespace: "test-namespace",
		Values:    inputValues,
		Version:   "0.0.1",
	})
	require.NoError(t, err)

	actual, err := root.Render(nil, helmette.Release{
		Name:      "subchart",
		Namespace: "test-namespace",
		IsInstall: true,
		Service:   "Helm",
	}, inputValues)
	require.NoError(t, err)

	actualByte := convertToString(actual)

	actualDocuments, err := ytbx.LoadDocuments(actualByte)
	require.NoError(t, err)

	expectedDocuments, err := ytbx.LoadDocuments(expected)
	require.NoError(t, err)

	t.Logf("Actual:\n%s", actualByte)
	t.Logf("Expected:\n%s", expected)

	sorter := func(a, b *yamlv3.Node) int {
		aNode, err := ytbx.Grab(a, "data.values")
		require.NoError(t, err)
		bNode, err := ytbx.Grab(b, "data.values")
		require.NoError(t, err)
		return strings.Compare(aNode.Value, bNode.Value)
	}
	slices.SortStableFunc(actualDocuments, sorter)
	slices.SortStableFunc(expectedDocuments, sorter)

	report, err := dyff.CompareInputFiles(
		ytbx.InputFile{Documents: expectedDocuments},
		ytbx.InputFile{Documents: actualDocuments},
	)
	if err != nil {
		require.NoError(t, err)
	}

	if len(report.Diffs) > 0 {
		hr := dyff.HumanReport{Report: report, OmitHeader: true}

		var buf bytes.Buffer
		require.NoError(t, hr.WriteReport(&buf))

		require.Fail(t, buf.String())
	}
}

func TestWriteArchive(t *testing.T) {
	charts := getTestCharts()
	root := charts[0]

	temp := t.TempDir()
	out, err := exec.Command("helm", "package", "testdata/subchart/root", "-u", "-d", temp).CombinedOutput()
	require.NoError(t, err)

	// helm package outputs:
	// Successfully packaged chart and saved it to: /full/path/to/dependency-0.0.1.tgz\n
	// This (nastily) extracts the path.
	helmArchive, err := os.ReadFile(string(out[bytes.LastIndex(out, []byte(": "))+2 : len(out)-1]))
	require.NoError(t, err)

	var goArchive bytes.Buffer
	require.NoError(t, root.WriteArchive(&goArchive))

	goFiles := map[string][]byte{}
	for header, content := range tarGzFiles(t, &goArchive) {
		goFiles[header.Name] = content
	}

	helmFiles := map[string][]byte{}
	for header, content := range tarGzFiles(t, bytes.NewReader(helmArchive)) {
		helmFiles[header.Name] = content
	}

	// Assert that the inflated outputs of `helm package` and `WriteArchive` are
	// identical.
	for fileName, goContent := range goFiles {
		helmContent, ok := helmFiles[fileName]
		assert.True(t, ok, "file %s not found in helm archive", fileName)
		assert.Equal(t, goContent, helmContent)
	}
	// cross-check that the files in the helm archive are the same as the files in the go archive.
	for fileName, helmContent := range helmFiles {
		goContent, ok := goFiles[fileName]
		assert.True(t, ok, "file %s not found in helm archive", fileName)
		assert.Equal(t, helmContent, goContent)
	}
}

func tarGzFiles(t *testing.T, reader io.Reader) iter.Seq2[*tar.Header, []byte] {
	gzipReader, err := gzip.NewReader(reader)
	require.NoError(t, err)

	tarReader := tar.NewReader(gzipReader)

	return func(yield func(*tar.Header, []byte) bool) {
		for {
			header, err := tarReader.Next()
			if err == io.EOF {
				return
			}
			if header.Typeflag != tar.TypeReg {
				continue
			}

			require.NoError(t, err)

			content, err := io.ReadAll(tarReader)
			require.NoError(t, err)

			if !yield(header, content) {
				break
			}
		}
	}
}

func convertToString(objs []kube.Object) []byte {
	b := bytes.NewBuffer(nil)
	for _, obj := range objs {
		fmt.Fprintf(b, "---\n%s\n", MustMarshalYAML(obj))
	}
	return b.Bytes()
}

func MustMarshalYAML(x any) string {
	bs, err := yaml.Marshal(x)
	if err != nil {
		panic(err)
	}
	return string(bs)
}

func renderConfigMap(dot *helmette.Dot) []kube.Object {
	return []kube.Object{
		&corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("%s-%s", dot.Chart.Name, dot.Release.Name),
			},
			Data: map[string]string{
				// TODO: would be nice to verify that release and chart are
				// equivalent but they're they only objects that aren't
				// represented as map[string]any which causes marshaling to
				// produce different results in helm and go.
				// "release": MustMarshalYAML(dot.Release),
				// "chart":   MustMarshalYAML(dot.Chart),
				"values": MustMarshalYAML(dot.Values),
			},
		},
	}
}
