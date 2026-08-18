// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package enterprisedrift

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The tests in this file are source-level drift guards: they parse Go files
// on both sides of the enterprise module boundary and compare declarations
// the enterprise module deliberately duplicates from OSS packages it cannot
// import. Like enterprise/operator/lifecycle/forkledger_test.go, they only
// work while the enterprise module lives inside the monorepo and skip
// themselves once it has been lifted out (see enterprise/README.md's lift
// runbook).

// repoRoot returns the monorepo root, three levels up from this package.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok, "resolving caller path")
	return filepath.Join(filepath.Dir(file), "..", "..", "..")
}

// skipUnlessEnterprisePresent skips t when the enterprise-side path is gone —
// the post-lift state, at which point these guards must be replaced with
// equivalents that pin against the released enterprise module version.
func skipUnlessEnterprisePresent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skipf("%s does not exist — assuming the enterprise module has been lifted out of the monorepo; replace this drift guard per enterprise/README.md's lift runbook", path)
	}
}

func parseGoFile(t *testing.T, fset *token.FileSet, path string) (*ast.File, []byte) {
	t.Helper()
	src, err := os.ReadFile(path)
	require.NoError(t, err, "reading %s", path)
	f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	require.NoError(t, err, "parsing %s", path)
	return f, src
}

// funcKey identifies a function declaration by "Receiver.Name" (or just
// "Name" for plain functions).
func funcKey(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return fd.Name.Name
	}
	recv := fd.Recv.List[0].Type
	if star, ok := recv.(*ast.StarExpr); ok {
		recv = star.X
	}
	if ident, ok := recv.(*ast.Ident); ok {
		return ident.Name + "." + fd.Name.Name
	}
	return fd.Name.Name
}

func funcsInFile(f *ast.File) map[string]*ast.FuncDecl {
	funcs := map[string]*ast.FuncDecl{}
	for _, decl := range f.Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok {
			funcs[funcKey(fd)] = fd
		}
	}
	return funcs
}

// printedFunc renders a function without any comments and with blank lines
// collapsed, so deliberate doc/interior-comment differences between the
// modules don't trip a guard while any code change does.
func printedFunc(t *testing.T, fset *token.FileSet, fd *ast.FuncDecl) string {
	t.Helper()
	clone := *fd
	clone.Doc = nil
	var buf bytes.Buffer
	require.NoError(t, printer.Fprint(&buf, fset, &clone))
	lines := make([]string, 0, 64)
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

// TestRollSafetyHelpersDrift pins the rolling-restart safety helpers that
// enterprise/operator/controller/roll_helpers.go duplicates from this
// repo's operator/internal/controller/redpanda/redpanda_controller.go. These
// functions encode the shared per-pod roll/no-roll decision table (RFC cases
// 2/3/4 — data-loss safety); if the copies drift, the OSS RedpandaReconciler
// and the enterprise MulticlusterReconciler start making different restart
// decisions. Comments are excluded from the comparison (the enterprise copy
// carries stretch-specific doc text); any code change must be ported across
// and this guard updated in the same commit.
func TestRollSafetyHelpersDrift(t *testing.T) {
	root := repoRoot(t)
	ossPath := filepath.Join(root, "operator", "internal", "controller", "redpanda", "redpanda_controller.go")
	entPath := filepath.Join(root, "enterprise", "operator", "controller", "roll_helpers.go")
	skipUnlessEnterprisePresent(t, entPath)

	fset := token.NewFileSet()
	ossFile, _ := parseGoFile(t, fset, ossPath)
	entFile, _ := parseGoFile(t, fset, entPath)
	ossFuncs := funcsInFile(ossFile)
	entFuncs := funcsInFile(entFile)

	for _, name := range []string{
		"brokerIDForPod",
		"decideRollAction",
		"brokerSafeToRestart",
		"brokerCaughtUp",
		"brokersStillRecovering",
	} {
		t.Run(name, func(t *testing.T) {
			ossFn, ok := ossFuncs[name]
			require.True(t, ok, "%s not found in %s; if it moved, update this guard", name, ossPath)
			entFn, ok := entFuncs[name]
			require.True(t, ok, "%s not found in %s; if it moved, update this guard", name, entPath)
			require.Equal(t, printedFunc(t, fset, ossFn), printedFunc(t, fset, entFn),
				"the OSS and enterprise copies of %s have drifted; port the change across (both roll loops must make identical restart-safety decisions)", name)
		})
	}
}

// TestTestutilMirrorDrift pins the test helpers that
// enterprise/pkg/testutil/testutil.go mirrors from pkg/testutil (which the
// enterprise module must not import). Low stakes — drift only skews test
// skipping/timeout behavior between the modules — but keeping the guard means
// every deliberate duplication across the boundary is covered.
func TestTestutilMirrorDrift(t *testing.T) {
	root := repoRoot(t)
	ossPath := filepath.Join(root, "pkg", "testutil", "testutil.go")
	entPath := filepath.Join(root, "enterprise", "pkg", "testutil", "testutil.go")
	skipUnlessEnterprisePresent(t, entPath)

	fset := token.NewFileSet()
	ossFile, _ := parseGoFile(t, fset, ossPath)
	entFile, _ := parseGoFile(t, fset, entPath)
	ossFuncs := funcsInFile(ossFile)
	entFuncs := funcsInFile(entFile)

	for _, name := range []string{
		"Context",
		"RequireTimeout",
		"SkipIfNotIntegration",
		"FreePorts",
	} {
		t.Run(name, func(t *testing.T) {
			ossFn, ok := ossFuncs[name]
			require.True(t, ok, "%s not found in %s; if it moved, update this guard", name, ossPath)
			entFn, ok := entFuncs[name]
			require.True(t, ok, "%s not found in %s; if it moved, update this guard", name, entPath)
			require.Equal(t, printedFunc(t, fset, ossFn), printedFunc(t, fset, entFn),
				"the pkg/testutil original and its enterprise mirror of %s have drifted; port the change across", name)
		})
	}
}

// forkedAPIFiles are the enterprise API files whose type declarations are
// verbatim forks of an OSS API package (see the NOTE headers in each file).
// Both CRD generators consume their own module's structs, so `task generate`
// keeps each side self-consistent and can never detect cross-module drift —
// this guard is the only thing that does. renames maps an enterprise type
// name back to its OSS original where the fork deliberately renamed it.
var forkedAPIFiles = []struct {
	name    string
	ossDir  []string
	renames map[string]string
}{
	{name: "chart_value_types.go", ossDir: []string{"operator", "api", "redpanda", "v1alpha2"}},
	{
		// Forked from vectorized/v1alpha1 (where the OSS redpanda/v1alpha2
		// package sources its ClusterConfiguration type from), with
		// ExternalSecretKeySelector renamed to avoid a clash.
		name:    "cluster_configuration_types.go",
		ossDir:  []string{"operator", "api", "vectorized", "v1alpha1"},
		renames: map[string]string{"ConfigExternalSecretKeySelector": "ExternalSecretKeySelector"},
	},
	{name: "common.go", ossDir: []string{"operator", "api", "redpanda", "v1alpha2"}},
}

// intentionalFuncDivergences lists fork-file methods whose implementations
// deliberately differ from their OSS originals (semantics preserved); each
// entry needs a reason.
var intentionalFuncDivergences = map[string]string{
	// OSS delegates to the DeepCopyPodSpecApplyConfiguration helper, which is
	// not part of the fork; the enterprise copy inlines the same JSON
	// round-trip.
	"PodTemplate.DeepCopy": "apply-configuration deep copy inlined instead of calling the OSS helper",
}

// TestForkedAPIValueTypesDrift compares every type declared in the enterprise
// fork files against the same-named type in its OSS source package,
// byte-for-byte including doc comments — kubebuilder validation markers live
// in comments, and the fork's contract is that the generated CRD schemas stay
// identical. Documented renames are normalized before comparing. Methods
// declared in the fork files are compared too (comment-free) when a
// same-named method exists OSS-side; enterprise-only helpers and the
// divergences listed in intentionalFuncDivergences are skipped.
func TestForkedAPIValueTypesDrift(t *testing.T) {
	root := repoRoot(t)
	entDir := filepath.Join(root, "enterprise", "operator", "api", "redpanda", "v1alpha2")
	skipUnlessEnterprisePresent(t, entDir)

	fset := token.NewFileSet()

	// Index every type declaration and function in each referenced OSS package.
	type ossIndex struct {
		types map[string]string
		funcs map[string]*ast.FuncDecl
	}
	ossIndexes := map[string]*ossIndex{}
	indexOSSDir := func(dir string) *ossIndex {
		if idx, ok := ossIndexes[dir]; ok {
			return idx
		}
		idx := &ossIndex{types: map[string]string{}, funcs: map[string]*ast.FuncDecl{}}
		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		for _, entry := range entries {
			if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			f, src := parseGoFile(t, fset, filepath.Join(dir, entry.Name()))
			collectTypeDecls(fset, f, src, idx.types)
			for key, fd := range funcsInFile(f) {
				idx.funcs[key] = fd
			}
		}
		ossIndexes[dir] = idx
		return idx
	}

	for _, fork := range forkedAPIFiles {
		ossDir := filepath.Join(root, filepath.Join(fork.ossDir...))
		oss := indexOSSDir(ossDir)
		entFile, entSrc := parseGoFile(t, fset, filepath.Join(entDir, fork.name))

		// unrename maps enterprise text back onto the OSS original's names so
		// documented renames don't register as drift.
		unrename := func(s string) string {
			for entName, ossName := range fork.renames {
				s = strings.ReplaceAll(s, entName, ossName)
			}
			return s
		}

		entTypes := map[string]string{}
		collectTypeDecls(fset, entFile, entSrc, entTypes)
		typeNames := make([]string, 0, len(entTypes))
		for typeName := range entTypes {
			typeNames = append(typeNames, typeName)
		}
		sort.Strings(typeNames)
		for _, typeName := range typeNames {
			t.Run(fork.name+"/"+typeName, func(t *testing.T) {
				ossName := typeName
				if renamed, ok := fork.renames[typeName]; ok {
					ossName = renamed
				}
				original, ok := oss.types[ossName]
				require.True(t, ok,
					"enterprise fork type %s has no original named %s in %s; if the OSS type was renamed or removed, reconcile the fork (and this guard) with it", typeName, ossName, ossDir)
				require.Equal(t, stripTODOLines(original), stripTODOLines(unrename(entTypes[typeName])),
					"the OSS original and enterprise fork of type %s have drifted (comparison includes doc comments — kubebuilder markers feed the CRD schemas); port the change across so both modules generate identical schemas", typeName)
			})
		}

		entFuncs := funcsInFile(entFile)
		funcKeys := make([]string, 0, len(entFuncs))
		for key := range entFuncs {
			funcKeys = append(funcKeys, key)
		}
		sort.Strings(funcKeys)
		for _, key := range funcKeys {
			if _, ok := intentionalFuncDivergences[key]; ok {
				continue
			}
			ossFn, ok := oss.funcs[key]
			if !ok {
				// Enterprise-only helper (e.g. a method the fork added); the
				// CRD-schema contract is carried by the type declarations.
				continue
			}
			entFn := entFuncs[key]
			t.Run(fork.name+"/"+key, func(t *testing.T) {
				require.Equal(t, printedFunc(t, fset, ossFn), unrename(printedFunc(t, fset, entFn)),
					"the OSS original and enterprise fork of %s have drifted; port the change across", key)
			})
		}
	}
}

// stripTODOLines drops `// TODO` comment lines before comparing type
// declarations: controller-gen strips them from the generated CRD
// descriptions, so they cannot affect the schema contract and the fork may
// omit them.
func stripTODOLines(s string) string {
	lines := make([]string, 0, 64)
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "// TODO") {
			continue
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// collectTypeDecls records each type declaration's raw source — including its
// doc comment, where kubebuilder markers live — keyed by type name.
func collectTypeDecls(fset *token.FileSet, f *ast.File, src []byte, out map[string]string) {
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			start, end := gd.Pos(), gd.End()
			doc := gd.Doc
			if len(gd.Specs) > 1 {
				start, end = ts.Pos(), ts.End()
				doc = ts.Doc
			}
			if doc != nil {
				start = doc.Pos()
			}
			out[ts.Name.Name] = string(src[fset.Position(start).Offset:fset.Position(end).Offset])
		}
	}
}
