// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// The off-the-shelf tools, each as one constructor that reads its settings
// block and returns an analyzer. newDepguard is the shortest; copy its shape.
package main

import (
	"go/token"
	"io"
	"log"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	depguard "github.com/OpenPeeDeeP/depguard/v2"
	"github.com/golangci/misspell"
	"github.com/hidalgopl/laconiccomments"
	"github.com/julz/importas"
	"github.com/securego/gosec/v2"
	"github.com/securego/gosec/v2/analyzers"
	"github.com/securego/gosec/v2/rules"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/appends"
	"golang.org/x/tools/go/analysis/passes/asmdecl"
	"golang.org/x/tools/go/analysis/passes/assign"
	"golang.org/x/tools/go/analysis/passes/atomic"
	"golang.org/x/tools/go/analysis/passes/bools"
	"golang.org/x/tools/go/analysis/passes/buildssa"
	"golang.org/x/tools/go/analysis/passes/buildtag"
	"golang.org/x/tools/go/analysis/passes/cgocall"
	"golang.org/x/tools/go/analysis/passes/composite"
	"golang.org/x/tools/go/analysis/passes/copylock"
	"golang.org/x/tools/go/analysis/passes/defers"
	"golang.org/x/tools/go/analysis/passes/directive"
	"golang.org/x/tools/go/analysis/passes/errorsas"
	"golang.org/x/tools/go/analysis/passes/framepointer"
	"golang.org/x/tools/go/analysis/passes/hostport"
	"golang.org/x/tools/go/analysis/passes/httpresponse"
	"golang.org/x/tools/go/analysis/passes/ifaceassert"
	"golang.org/x/tools/go/analysis/passes/inline"
	"golang.org/x/tools/go/analysis/passes/loopclosure"
	"golang.org/x/tools/go/analysis/passes/lostcancel"
	"golang.org/x/tools/go/analysis/passes/nilfunc"
	"golang.org/x/tools/go/analysis/passes/printf"
	"golang.org/x/tools/go/analysis/passes/shift"
	"golang.org/x/tools/go/analysis/passes/sigchanyzer"
	"golang.org/x/tools/go/analysis/passes/slog"
	"golang.org/x/tools/go/analysis/passes/stdmethods"
	"golang.org/x/tools/go/analysis/passes/stdversion"
	"golang.org/x/tools/go/analysis/passes/stringintconv"
	"golang.org/x/tools/go/analysis/passes/structtag"
	"golang.org/x/tools/go/analysis/passes/testinggoroutine"
	"golang.org/x/tools/go/analysis/passes/tests"
	"golang.org/x/tools/go/analysis/passes/timeformat"
	"golang.org/x/tools/go/analysis/passes/unmarshal"
	"golang.org/x/tools/go/analysis/passes/unreachable"
	"golang.org/x/tools/go/analysis/passes/unsafeptr"
	"golang.org/x/tools/go/analysis/passes/unusedresult"
	"golang.org/x/tools/go/analysis/passes/waitgroup"
	"golang.org/x/tools/go/packages"
	"honnef.co/go/tools/analysis/lint"
	"honnef.co/go/tools/quickfix"
	"honnef.co/go/tools/simple"
	"honnef.co/go/tools/staticcheck"
	"honnef.co/go/tools/stylecheck"
	"honnef.co/go/tools/unused"
	"mvdan.cc/unparam/check"
)

// vetDefaults is govet's enabled-by-default set, copied from golangci-lint's
// pkg/golinters/govet so that "govet" means the same thing in both tools. The
// off-by-default ones (shadow, fieldalignment, nilness, ...) stay off.
var vetDefaults = []*analysis.Analyzer{
	appends.Analyzer,
	asmdecl.Analyzer,
	assign.Analyzer,
	atomic.Analyzer,
	bools.Analyzer,
	buildtag.Analyzer,
	cgocall.Analyzer,
	composite.Analyzer,
	copylock.Analyzer,
	defers.Analyzer,
	directive.Analyzer,
	errorsas.Analyzer,
	framepointer.Analyzer,
	hostport.Analyzer,
	httpresponse.Analyzer,
	ifaceassert.Analyzer,
	inline.Analyzer,
	loopclosure.Analyzer,
	lostcancel.Analyzer,
	nilfunc.Analyzer,
	printf.Analyzer,
	shift.Analyzer,
	sigchanyzer.Analyzer,
	slog.Analyzer,
	stdmethods.Analyzer,
	stdversion.Analyzer,
	stringintconv.Analyzer,
	structtag.Analyzer,
	testinggoroutine.Analyzer,
	tests.Analyzer,
	timeformat.Analyzer,
	unmarshal.Analyzer,
	unreachable.Analyzer,
	unsafeptr.Analyzer,
	unusedresult.Analyzer,
	waitgroup.Analyzer,
}

// depguardSettings is linters.settings.depguard, in golangci-lint's keys.
type depguardSettings struct {
	Rules map[string]struct {
		ListMode string   `json:"list-mode"`
		Files    []string `json:"files"`
		Allow    []string `json:"allow"`
		Deny     []struct {
			Pkg  string `json:"pkg"`
			Desc string `json:"desc"`
		} `json:"deny"`
	} `json:"rules"`
}

// newDepguard is the shortest example of a tool with settings. Mind depguard's
// syntax: deny keys are path prefixes, a trailing $ means exact, and a leading
// ^ is a literal that never matches.
func newDepguard() *analysis.Analyzer {
	var s depguardSettings
	settings("depguard", &s)

	lists := depguard.LinterSettings{}
	for name, rule := range s.Rules {
		deny := map[string]string{}
		for _, d := range rule.Deny {
			deny[d.Pkg] = d.Desc
		}

		lists[name] = &depguard.List{ListMode: rule.ListMode, Files: rule.Files, Allow: rule.Allow, Deny: deny}
	}

	a := depguard.NewUncompiledAnalyzer(&lists)
	if err := a.Compile(); err != nil {
		fatalf("settings for depguard: %v", err)
	}

	return a.Analyzer
}

// importasSettings is linters.settings.importas, in golangci-lint's keys.
type importasSettings struct {
	NoUnaliased    bool `json:"no-unaliased"`
	NoExtraAliases bool `json:"no-extra-aliases"`
	Alias          []struct {
		Pkg   string `json:"pkg"`
		Alias string `json:"alias"`
	} `json:"alias"`
}

// newImportas configures upstream through its flags, as golangci-lint did.
func newImportas() *analysis.Analyzer {
	var s importasSettings
	settings("importas", &s)

	a := importas.Analyzer
	set := func(flag, value string) {
		if err := a.Flags.Set(flag, value); err != nil {
			fatalf("settings for importas: -%s=%s: %v", flag, value, err)
		}
	}

	set("no-unaliased", strconv.FormatBool(s.NoUnaliased))
	set("no-extra-aliases", strconv.FormatBool(s.NoExtraAliases))

	for _, alias := range s.Alias {
		set("alias", alias.Pkg+":"+alias.Alias)
	}

	return a
}

// newLaconiccomments reads linters.settings.custom.laconiccomments.settings,
// where golangci-lint keeps a module plugin's settings, so the file needs no
// change if this linter ever runs under golangci-lint again.
func newLaconiccomments() *analysis.Analyzer {
	s := laconiccomments.DefaultSettings()
	settings(laconiccomments.Name, &s)

	return laconiccomments.New(s)
}

// newUnparam runs unparam's checker one package at a time, as golangci-lint
// did. Its own binary runs whole-program instead and reports 5 fewer closures
// on this repo; per package is the setting CI enforced.
func newUnparam() *analysis.Analyzer {
	var s struct {
		CheckExported bool `json:"check-exported"`
	}
	settings("unparam", &s)

	return &analysis.Analyzer{
		Name:     "unparam",
		Doc:      "Reports unused function parameters and results",
		Requires: []*analysis.Analyzer{buildssa.Analyzer},
		Run: func(pass *analysis.Pass) (any, error) {
			checker := &check.Checker{}
			checker.CheckExportedFuncs(s.CheckExported)
			checker.Packages([]*packages.Package{packageOf(pass)})
			checker.ProgramSSA(pass.ResultOf[buildssa.Analyzer].(*buildssa.SSA).Pkg.Prog)

			issues, err := checker.Check()
			if err != nil {
				return nil, err
			}

			for _, issue := range issues {
				pass.Reportf(issue.Pos(), "%s", issue.Message())
			}

			return nil, nil
		},
	}
}

// newMisspell checks every file's full text, which is golangci-lint's default
// mode for this linter (its "restricted" mode would look at comments and
// strings only).
func newMisspell() *analysis.Analyzer {
	var s struct {
		IgnoreRules []string `json:"ignore-rules"`
	}
	settings("misspell", &s)

	replacer := misspell.New()
	replacer.RemoveRule(s.IgnoreRules)
	replacer.Compile()

	return &analysis.Analyzer{
		Name: "misspell",
		Doc:  "Finds commonly misspelled English words",
		Run: func(pass *analysis.Pass) (any, error) {
			for _, f := range pass.Files {
				file := pass.Fset.File(f.FileStart)

				src, err := pass.ReadFile(file.Name())
				if err != nil {
					return nil, err
				}

				_, diffs := replacer.Replace(string(src))
				for _, d := range diffs {
					if pos := posOf(pass, file.Name(), d.Line, d.Column+1); pos != token.NoPos {
						pass.Reportf(pos, "%q is a misspelling of %q", d.Original, d.Corrected)
					}
				}
			}

			return nil, nil
		},
	}
}

// gosecSettings is linters.settings.gosec, in golangci-lint's keys.
type gosecSettings struct {
	Includes []string       `json:"includes"`
	Excludes []string       `json:"excludes"`
	Config   map[string]any `json:"config"`
}

// newGosec runs gosec's rule engine and its SSA analyzers per package. The
// _test.go exclusion from the old config is an exclusions.rules entry.
func newGosec() *analysis.Analyzer {
	var s gosecSettings
	settings("gosec", &s)

	conf := gosec.NewConfig()
	for key, value := range s.Config {
		conf.Set(strings.ToUpper(key), value)
	}

	// G407 is excluded unconditionally, as golangci-lint does.
	excludes := append([]string{"G407"}, s.Excludes...)

	var ruleFilters []rules.RuleFilter
	var analyzerFilters []analyzers.AnalyzerFilter

	if len(s.Includes) > 0 {
		ruleFilters = append(ruleFilters, rules.NewRuleFilter(false, s.Includes...))
		analyzerFilters = append(analyzerFilters, analyzers.NewAnalyzerFilter(false, s.Includes...))
	}

	ruleList := rules.Generate(false, append(ruleFilters, rules.NewRuleFilter(true, excludes...))...)
	analyzerList := analyzers.Generate(false, append(analyzerFilters, analyzers.NewAnalyzerFilter(true, excludes...))...)

	return &analysis.Analyzer{
		Name:     "gosec",
		Doc:      "Inspects source code for security problems",
		Requires: []*analysis.Analyzer{buildssa.Analyzer},
		Run: func(pass *analysis.Pass) (any, error) {
			// A fresh engine per pass: it accumulates issues and is not safe to share.
			engine := gosec.NewAnalyzer(conf, true, false, false, 1, log.New(io.Discard, "", 0))
			engine.LoadRules(ruleList.RulesInfo())
			engine.LoadAnalyzers(analyzerList.AnalyzersInfo())

			pkg := packageOf(pass)
			engine.CheckRules(pkg)
			engine.CheckAnalyzersWithSSA(pkg, pass.ResultOf[buildssa.Analyzer].(*buildssa.SSA))

			found, _, _ := engine.Report()
			for _, i := range found {
				// gosec positions are text; Line may be a "12-14" range.
				first, _, _ := strings.Cut(i.Line, "-")
				line, _ := strconv.Atoi(first)
				column, _ := strconv.Atoi(i.Col)

				if pos := posOf(pass, i.File, line, column); pos != token.NoPos {
					pass.Reportf(pos, "%s: %s", i.RuleID, i.What)
				}
			}

			return nil, nil
		},
	}
}

// staticcheckAnalyzers is honnef's four families, selected by
// linters.settings.staticcheck.checks. golangci-lint's staticcheck is exactly
// these four; unused (U1000) is its own linter, see newUnused.
func staticcheckAnalyzers() []*analysis.Analyzer {
	var all []*analysis.Analyzer

	for _, family := range [][]*lint.Analyzer{staticcheck.Analyzers, simple.Analyzers, stylecheck.Analyzers, quickfix.Analyzers} {
		for _, a := range family {
			all = append(all, a.Analyzer)
		}
	}

	var s struct {
		Checks []string `json:"checks"`
	}
	settings("staticcheck", &s)

	on := selected(all, s.Checks)

	var out []*analysis.Analyzer
	for _, a := range all {
		if on[a.Name] {
			out = append(out, a)
		}
	}

	return out
}

// selected evaluates a checks list the way golangci-lint does
// (pkg/golinters/staticcheck.filterAnalyzerNames): entries apply in order,
// "all" or "*" is everything, "-X" removes, "S*" is a family and "S1*" a
// prefix. An empty list is honnef's default.
func selected(all []*analysis.Analyzer, checks []string) map[string]bool {
	if len(checks) == 0 {
		checks = []string{"all", "-ST1000", "-ST1003", "-ST1016", "-ST1020", "-ST1021", "-ST1022"}
	}

	on := map[string]bool{}

	for _, check := range checks {
		want := true
		if strings.HasPrefix(check, "-") {
			want, check = false, check[1:]
		}

		for _, a := range all {
			if matches(a.Name, check) {
				on[a.Name] = want
			}
		}
	}

	return on
}

func matches(name, check string) bool {
	switch prefix := strings.TrimSuffix(check, "*"); {
	case check == "all" || check == "*":
		return true
	case prefix == check: // no wildcard
		return name == check
	case strings.IndexFunc(prefix, unicode.IsDigit) == -1: // a family: S* is S1000 but not SA1000
		digits := strings.IndexFunc(name, unicode.IsDigit)

		return digits >= 0 && name[:digits] == prefix
	default:
		return strings.HasPrefix(name, prefix)
	}
}

// newUnused reports honnef's U1000 under the name golangci-lint gave it, so
// the repo's //nolint:unused directives keep working. U1000 itself returns a
// Result rather than reporting; this reads that result off the pass.
func newUnused() *analysis.Analyzer {
	return &analysis.Analyzer{
		Name:     "unused",
		Doc:      "Reports unused constants, variables, functions, types and fields",
		URL:      "https://staticcheck.dev/docs/checks#U1000",
		Requires: []*analysis.Analyzer{unused.Analyzer.Analyzer},
		Run:      runUnused,
	}
}

func runUnused(pass *analysis.Pass) (any, error) {
	// golangci-lint analyses each package's test-inclusive variant only, so a
	// helper used solely from its own tests counts as used. go vet does the
	// same. A standalone run also loads the plain variant; skip it when tests
	// exist, or every such helper is reported twice over and wrongly.
	if isPlainVariantOfTestedPackage(pass) {
		return nil, nil
	}

	result := pass.ResultOf[unused.Analyzer.Analyzer].(unused.Result)

	used := map[token.Position]bool{}
	for _, o := range result.Used {
		used[o.Position] = true
	}

	for _, o := range result.Unused {
		if o.Kind == "type param" || used[o.Position] {
			continue
		}

		if pos := posOf(pass, o.Position.Filename, o.Position.Line, o.Position.Column); pos != token.NoPos {
			pass.Reportf(pos, "%s %s is unused", o.Kind, o.Name)
		}
	}

	return nil, nil
}

func isPlainVariantOfTestedPackage(pass *analysis.Pass) bool {
	if len(pass.Files) == 0 {
		return false
	}

	for _, f := range pass.Files {
		if strings.HasSuffix(pass.Fset.File(f.FileStart).Name(), "_test.go") {
			return false
		}
	}

	tests, _ := filepath.Glob(filepath.Join(filepath.Dir(pass.Fset.File(pass.Files[0].FileStart).Name()), "*_test.go"))

	return len(tests) > 0
}

// Two helpers shared by the embedded tools.

// packageOf is the view of a pass that tools built around go/packages expect.
func packageOf(pass *analysis.Pass) *packages.Package {
	return &packages.Package{Fset: pass.Fset, Syntax: pass.Files, Types: pass.Pkg, TypesInfo: pass.TypesInfo}
}

// posOf converts a file/line/column triple back into a token.Pos, which is
// what a Diagnostic carries. Several embedded tools report positions as text.
func posOf(pass *analysis.Pass, filename string, line, column int) token.Pos {
	for _, f := range pass.Files {
		file := pass.Fset.File(f.FileStart)
		if file.Name() != filename || line < 1 || line > file.LineCount() {
			continue
		}

		return file.LineStart(line) + token.Pos(max(column-1, 0))
	}

	return token.NoPos
}
