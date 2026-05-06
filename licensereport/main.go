// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Command licensereport enumerates a Go module's third-party dependencies
// and emits a markdown table of (module, license type, source URL) — the
// data shape used by licenses/third_party.md.
//
// Replaces github.com/google/go-licenses, which is unmaintained and depends
// on vanity-URL meta-tag lookups (hello, gopkg.in) for URL resolution. This
// tool resolves URLs via proxy.golang.org's Origin field, classifies licenses
// with Google's licenseclassifier, and falls back to a previously-committed
// table when the proxy lacks Origin (older entries cached pre-2021).
package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	classifier "github.com/google/licenseclassifier/v2"
	"github.com/google/licenseclassifier/v2/assets"
)

const proxyBase = "https://proxy.golang.org"

type proxyInfo struct {
	Version string
	Time    string
	Origin  *struct {
		VCS    string
		URL    string
		Subdir string // path within the repo where the module lives, if not at root
		Hash   string
		Ref    string
	}
}

type goListPackage struct {
	ImportPath string
	Standard   bool
	Module     *struct {
		Path     string
		Version  string
		Main     bool
		Indirect bool
		// Replace, when set, indicates the module is being substituted via
		// a replace directive in go.mod. The actual code shipped comes
		// from Replace.Path@Replace.Version, so license URLs must point
		// there even though the import path stays the original.
		Replace *struct {
			Path    string
			Version string
		}
	}
	Imports      []string
	TestImports  []string
	XTestImports []string
}

type module struct {
	Path    string
	Version string
}

// emitRow is one rendered output line for a (package, license-file) pair.
type emitRow struct {
	Name        string // "github.com/X/Y" or sub-package path
	LicenseName string // SPDX identifier or "Unknown"
	URL         string // SHA-pinned blob URL or fallback URL
}

// classifyResult comes from the LICENSE-classification step for a module.
type classifyResult struct {
	relPath    string // e.g. "LICENSE" or "internal/x/LICENSE"
	licenseDir string // e.g. "" or "internal/x"
	spdx       string
	confidence float64
}

func main() {
	var (
		ignoreFlags  multiFlag
		fallbackFile string
		dir          string
		concurrency  int
		threshold    float64
		validateFull bool
	)
	flag.Var(&ignoreFlags, "ignore", "Module path prefix to omit (repeatable)")
	flag.StringVar(&fallbackFile, "fallback-from", "", "Path to existing licenses/third_party.md to mine for URL fallbacks when proxy lacks Origin")
	flag.StringVar(&dir, "dir", ".", "Directory to run `go list` in")
	flag.IntVar(&concurrency, "concurrency", 16, "Parallel proxy fetches")
	flag.Float64Var(&threshold, "confidence", 0.85, "Minimum classifier confidence; below this emit Unknown")
	flag.BoolVar(&validateFull, "validate-full", false, "HEAD-check every URL even if it appeared in --fallback-from. Off by default: URLs already in the previously-committed file are assumed valid (trust the prior run). Turn on for a periodic full audit.")
	flag.Parse()

	fallback := map[string]string{} // module path -> URL
	if fallbackFile != "" {
		var err error
		fallback, err = parseFallback(fallbackFile)
		if err != nil {
			die("parse fallback: %v", err)
		}
		if !validateFull {
			// Trust URLs from the prior committed file: if they're
			// already in here, the previous run validated them. Skip
			// the HEAD-check and just treat them as known-good.
			urlExistsCacheMu.Lock()
			for _, u := range fallback {
				urlExistsCache[u] = true
			}
			urlExistsCacheMu.Unlock()
		}
	}

	pkgs, err := listPackages(dir)
	if err != nil {
		die("go list: %v", err)
	}

	// Filter: drop stdlib, the main module, and ignored prefixes.
	type pkgEntry struct {
		ImportPath string
		Module     module
	}
	var kept []pkgEntry
	moduleSet := map[module]struct{}{}
	for _, p := range pkgs {
		if p.Standard || p.Module == nil || p.Module.Main {
			continue
		}
		skip := false
		for _, ig := range ignoreFlags {
			if strings.HasPrefix(p.Module.Path, ig) || strings.HasPrefix(p.ImportPath, ig) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		// Follow replace directives: the LICENSE we ship comes from the
		// substituted module, not the original. The import path stays
		// the original (what users `import`), but proxy lookups and
		// LICENSE classification all use the replacement.
		modPath, modVersion := p.Module.Path, p.Module.Version
		if p.Module.Replace != nil && p.Module.Replace.Path != "" {
			modPath, modVersion = p.Module.Replace.Path, p.Module.Replace.Version
		}
		m := module{Path: modPath, Version: modVersion}
		kept = append(kept, pkgEntry{ImportPath: p.ImportPath, Module: m})
		moduleSet[m] = struct{}{}
	}

	// Resolve license info for every distinct module in parallel.
	resolved := resolveModules(moduleSet, concurrency, threshold)

	// For each kept package, find its nearest LICENSE within its module,
	// and bucket by (module, license-relative-path, license-spdx). A
	// single LICENSE file with multiple detected licenses produces one
	// row per license type, all sharing the same URL.
	type bucketKey struct {
		modulePath string
		licRelPath string
		spdx       string
	}
	type bucket struct {
		module      module
		license     classifyResult
		moduleInfo  *proxyInfo
		importPaths []string
	}
	buckets := map[bucketKey]*bucket{}
	for _, e := range kept {
		mr, ok := resolved[e.Module]
		if !ok {
			continue
		}
		rel := strings.TrimPrefix(e.ImportPath, e.Module.Path)
		rel = strings.TrimPrefix(rel, "/")
		lics := nearestLicenses(mr.licenses, rel)
		if len(lics) == 0 {
			continue
		}
		for _, lic := range lics {
			k := bucketKey{e.Module.Path, lic.relPath, lic.spdx}
			b, ok := buckets[k]
			if !ok {
				b = &bucket{module: e.Module, license: lic, moduleInfo: mr.info}
				buckets[k] = b
			}
			b.importPaths = append(b.importPaths, e.ImportPath)
		}
	}

	rows := make([]emitRow, 0, len(buckets))
	for _, b := range buckets {
		name := longestCommonPath(b.importPaths)
		if name == "" {
			name = b.module.Path
		}
		url := buildURL(b.moduleInfo, b.license.relPath, b.module, name, fallback)
		rows = append(rows, emitRow{
			Name:        name,
			LicenseName: b.license.spdx,
			URL:         url,
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Name != rows[j].Name {
			return rows[i].Name < rows[j].Name
		}
		if rows[i].LicenseName != rows[j].LicenseName {
			return rows[i].LicenseName < rows[j].LicenseName
		}
		return rows[i].URL < rows[j].URL
	})

	// Emit the file header so this output IS the full file content. Drops
	// the .tpl template indirection and the hardcoded overrides it carried
	// for go-licenses' broken cases.
	fmt.Println("# Licenses list")
	fmt.Println()
	fmt.Println("<!--")
	fmt.Println()
	fmt.Println("This list can be auto generated with licensereport.")
	fmt.Println()
	fmt.Println("Run `task generate:third-party-licenses-list`.")
	fmt.Println()
	fmt.Println("-->")
	fmt.Println()
	fmt.Println("# Go deps _used_ in production in K8S (exclude all test dependencies)")
	fmt.Println()
	fmt.Println("| software     | license        |")
	fmt.Println("| :----------: | :------------: |")
	for _, r := range rows {
		if r.URL == "" {
			fmt.Printf("| %s | [%s]() |\n", r.Name, r.LicenseName)
		} else {
			fmt.Printf("| %s | [%s](%s) |\n", r.Name, r.LicenseName, r.URL)
		}
	}
}

// moduleResult bundles the proxy info + classified licenses for one module.
type moduleResult struct {
	info     *proxyInfo
	licenses []classifyResult
}

func resolveModules(set map[module]struct{}, concurrency int, threshold float64) map[module]moduleResult {
	out := make(map[module]moduleResult, len(set))
	var mu sync.Mutex
	jobs := make(chan module)
	var wg sync.WaitGroup
	cls, err := assets.DefaultClassifier()
	if err != nil {
		die("classifier: %v", err)
	}
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for m := range jobs {
				info, _ := fetchInfo(m)
				z, err := fetchZip(m)
				if err != nil {
					fmt.Fprintf(os.Stderr, "licensereport: zip %s@%s: %v\n", m.Path, m.Version, err)
					continue
				}
				lics := classifyZipLicenses(z, cls, threshold)
				mu.Lock()
				out[m] = moduleResult{info: info, licenses: lics}
				mu.Unlock()
			}
		}()
	}
	for m := range set {
		jobs <- m
	}
	close(jobs)
	wg.Wait()
	return out
}

func classifyZipLicenses(z *zip.Reader, cls *classifier.Classifier, threshold float64) []classifyResult {
	// Pick the canonical LICENSE file per directory. A repo may ship
	// COPYING.md plus LICENSE.BSD plus LICENSE.MPL-2.0 (cyphar/filepath-securejoin
	// is the canonical example); go-licenses processes only the highest-
	// priority one per directory, so we do the same. Otherwise we'd emit
	// duplicate (module, license) rows for the same legal info.
	type candidate struct {
		base    string
		rel     string
		zipFile *zip.File
	}
	bestPerDir := map[string]candidate{}
	for _, f := range z.File {
		if f.FileInfo().IsDir() {
			continue
		}
		base := strings.ToUpper(path.Base(f.Name))
		if !isLicenseFile(base) {
			continue
		}
		// Strip "<module-path>@<version>/" from the start to get the
		// in-module relative path (e.g., "spdy/LICENSE" or "LICENSE").
		idx := strings.Index(f.Name, "@")
		if idx < 0 {
			continue
		}
		slash := strings.Index(f.Name[idx:], "/")
		if slash < 0 {
			continue
		}
		rel := f.Name[idx+slash+1:]
		dir := path.Dir(rel)
		if dir == "." {
			dir = ""
		}
		c := candidate{base: base, rel: rel, zipFile: f}
		if existing, ok := bestPerDir[dir]; !ok || licenseFilePriority(base) < licenseFilePriority(existing.base) {
			bestPerDir[dir] = c
		}
	}

	var out []classifyResult
	for _, c := range bestPerDir {
		f := c.zipFile
		rel := c.rel

		rc, err := f.Open()
		if err != nil {
			continue
		}
		buf, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			continue
		}

		matches := cls.Match(buf)
		// A single LICENSE file may declare multiple licenses (e.g. an
		// Apache-2.0 LICENSE with a BSD-3-Clause notice for embedded code).
		// Emit one classifyResult per License-type match above threshold;
		// dedupe by SPDX since the classifier sometimes returns the same
		// license multiple times with different fragment offsets.
		seenSpdx := map[string]struct{}{}
		var emitted bool
		for _, m := range matches.Matches {
			if m.MatchType != "License" || m.Confidence < threshold {
				continue
			}
			if _, dup := seenSpdx[m.Name]; dup {
				continue
			}
			seenSpdx[m.Name] = struct{}{}
			out = append(out, classifyResult{
				relPath:    rel,
				licenseDir: path.Dir(rel),
				spdx:       m.Name,
				confidence: m.Confidence,
			})
			emitted = true
		}
		if !emitted {
			out = append(out, classifyResult{
				relPath:    rel,
				licenseDir: path.Dir(rel),
				spdx:       "Unknown",
				confidence: 0,
			})
		}
	}
	// Normalize: path.Dir("LICENSE") == "." but our nearestLicense uses ""
	// for the module root. Convert.
	for i := range out {
		if out[i].licenseDir == "." {
			out[i].licenseDir = ""
		}
	}
	return out
}

// isLicenseFile matches the conventional LICENSE/COPYING/COPYRIGHT names,
// optionally with a suffix (LICENSE.txt, LICENSE.md, LICENSE.MIT, etc.).
// Projects that ship under multiple licenses sometimes include separate
// LICENSE.<SPDX> files; we want to find all of them.
func isLicenseFile(upperBase string) bool {
	for _, root := range []string{"LICENSE", "LICENCE", "COPYING", "COPYRIGHT"} {
		if upperBase == root {
			return true
		}
		if strings.HasPrefix(upperBase, root+".") || strings.HasPrefix(upperBase, root+"-") {
			return true
		}
	}
	return false
}

// licenseFilePriority orders candidate LICENSE filenames within one directory.
// Lower number wins. The classifier reads one file per directory, so when a
// repo ships several (LICENSE + LICENSE.BSD + LICENSE.MPL-2.0), we want the
// most general one — typically the bare LICENSE / COPYING — since it usually
// contains the full text of every license that applies.
func licenseFilePriority(upperBase string) int {
	switch upperBase {
	case "LICENSE", "LICENCE", "COPYING", "COPYRIGHT":
		return 0
	case "LICENSE.TXT", "LICENSE.MD", "LICENCE.TXT", "LICENCE.MD",
		"COPYING.TXT", "COPYING.MD", "COPYRIGHT.TXT":
		return 1
	}
	// LICENSE.<SPDX> or LICENSE-<SPDX> variants — last resort, since any
	// repo shipping these typically also has a more general file.
	return 2
}

// nearestLicenses walks up the directory chain from pkgRel and returns ALL
// classify results for the first ancestor directory that has any. (A single
// LICENSE file may produce multiple results — one per detected license type.)
func nearestLicenses(lics []classifyResult, pkgRel string) []classifyResult {
	dir := pkgRel
	for {
		var hits []classifyResult
		for i := range lics {
			if lics[i].licenseDir == dir {
				hits = append(hits, lics[i])
			}
		}
		if len(hits) > 0 {
			return hits
		}
		if dir == "" {
			return nil
		}
		idx := strings.LastIndex(dir, "/")
		if idx < 0 {
			dir = ""
		} else {
			dir = dir[:idx]
		}
	}
}

func buildURL(info *proxyInfo, licenseRelPath string, m module, name string, fallback map[string]string) string {
	if info != nil && info.Origin != nil && info.Origin.URL != "" {
		repoURL := rewriteRepoURL(info.Origin.URL)
		// Prefer the tag (refs/tags/...) when available — keeps URLs
		// human-readable and matches how go-licenses generated them.
		// Fall back to SHA for pseudo-versions.
		var ref string
		if tag := strings.TrimPrefix(info.Origin.Ref, "refs/tags/"); tag != info.Origin.Ref && tag != "" {
			ref = tag
		} else if info.Origin.Hash != "" {
			// 12-char short SHA — matches Go pseudo-version convention
			// and keeps the file compact.
			ref = info.Origin.Hash
			if len(ref) > 12 {
				ref = ref[:12]
			}
		}
		if ref != "" {
			// HEAD-check candidate paths from most-specific to root and
			// pick the first that 200s. Different repos handle subdir
			// modules differently:
			//   - aws-sdk-go-v2/internal/endpoints/v2 has a physical
			//     /v2 directory containing its own LICENSE.txt
			//   - cloud.google.com/go/auth has Subdir=auth but the
			//     LICENSE is inherited from the repo root
			//   - evanphx/json-patch/v5 has no Subdir and /v5 is virtual
			// Naively trusting Subdir or naively trusting /vN both
			// produce 404s; the only reliable option is to ask the
			// source-of-truth host (github, gitiles, gitlab, etc.).
			candidates := candidateRepoPaths(info.Origin.Subdir, m.Path, licenseRelPath)
			for _, p := range candidates {
				url := blobURL(repoURL, ref, p)
				if urlExists(url) {
					return url
				}
			}
			// All candidates 404'd — emit the LAST candidate (root) as
			// the safest fallback (license inherited from root is the
			// most common scenario when subdir lookups fail).
			fmt.Fprintf(os.Stderr, "licensereport: no working URL for %s@%s (tried %d candidates)\n", m.Path, m.Version, len(candidates))
			return blobURL(repoURL, ref, candidates[len(candidates)-1])
		}
	}
	// No Origin — fall back to a previously-committed URL. Verify it works;
	// if the fallback is itself broken (the file we copied from had bad
	// URLs for some entries), try common rewrites: strip /v<N>/ for
	// virtual major-version dirs, swap go.googlesource.com → github mirror.
	rawFallback, fallbackKey := "", ""
	if url, ok := fallback[name]; ok {
		rawFallback, fallbackKey = url, name
	} else if url, ok := fallback[m.Path]; ok {
		rawFallback, fallbackKey = url, m.Path
	}
	if rawFallback == "" {
		fmt.Fprintf(os.Stderr, "licensereport: no URL for %s@%s (proxy lacks Origin, no fallback)\n", m.Path, m.Version)
		return ""
	}
	for _, candidate := range fallbackCandidates(rawFallback, m.Path) {
		if urlExists(candidate) {
			if candidate != rawFallback {
				fmt.Fprintf(os.Stderr, "licensereport: rewrote broken fallback URL for %s\n", fallbackKey)
			}
			return candidate
		}
	}
	fmt.Fprintf(os.Stderr, "licensereport: keeping unverified fallback for %s (HEAD-check failed)\n", fallbackKey)
	return rawFallback
}

// rewriteRepoURL is a placeholder for any future host rewrites. Currently
// returns the URL unchanged — go.googlesource.com URLs are handled via
// blobURL, which uses Gitiles' /+/ syntax instead of /blob/.
func rewriteRepoURL(u string) string {
	return u
}

// blobURL constructs a viewable URL for a file at a given ref in a repo.
// Uses /blob/<ref>/<path> for github-style hosts and /+/<ref>/<path> for
// Gitiles hosts (go.googlesource.com).
func blobURL(repo, ref, path string) string {
	if strings.HasPrefix(repo, "https://go.googlesource.com/") {
		return fmt.Sprintf("%s/+/%s/%s", repo, ref, path)
	}
	return fmt.Sprintf("%s/blob/%s/%s", repo, ref, path)
}

// fallbackCandidates derives alternative URLs to try when the prior
// committed third_party.md row's URL doesn't resolve. Common transforms:
//
//   - strip a virtual /v<N>/ segment (modules whose path ends in /vN
//     where the major-version dir doesn't physically exist in the repo)
//   - rewrite go.googlesource.com hosts to the github mirror
func fallbackCandidates(url, modulePath string) []string {
	out := []string{url}
	// go.googlesource.com uses Gitiles' /+/ URL syntax. The previously-
	// committed file had /blob/ URLs (broken). Rewrite to Gitiles.
	if strings.Contains(url, "go.googlesource.com/") && strings.Contains(url, "/blob/") {
		out = append(out, strings.Replace(url, "/blob/", "/+/", 1))
	}
	if v := majorVersionSuffix(modulePath); v != "" {
		// Strip "/vN/" before the file name. Pattern: /<ref>/vN/<file>
		// → /<ref>/<file>. Only safe when the module path ends in /vN.
		needle := "/" + v + "/"
		if i := strings.LastIndex(url, needle); i >= 0 {
			out = append(out, url[:i+1]+url[i+len(needle):])
		}
	}
	return out
}

// majorVersionSuffix returns "vN" if the module path ends in a major-version
// directory suffix (/v2, /v3, ...); empty otherwise. Major-version v0/v1
// don't get a suffix in Go's semantic-import-versioning rules.
func majorVersionSuffix(modulePath string) string {
	idx := strings.LastIndex(modulePath, "/")
	if idx < 0 {
		return ""
	}
	last := modulePath[idx+1:]
	if len(last) < 2 || last[0] != 'v' {
		return ""
	}
	for _, r := range last[1:] {
		if r < '0' || r > '9' {
			return ""
		}
	}
	if last == "v0" || last == "v1" {
		return ""
	}
	return last
}

// candidateRepoPaths builds the ordered list of repo-relative file paths
// where the LICENSE might live, from most-specific (Subdir + physical /vN)
// to least-specific (repo root). The caller HEAD-checks them in order.
func candidateRepoPaths(subdir, modulePath, licenseRelPath string) []string {
	var out []string
	majorV := majorVersionSuffix(modulePath)
	if subdir != "" && majorV != "" {
		out = append(out, joinPath(subdir, majorV, licenseRelPath))
	}
	if subdir != "" {
		out = append(out, joinPath(subdir, licenseRelPath))
	}
	if majorV != "" && subdir == "" {
		// modules like github.com/X/Y/v2 with no Subdir — try /vN as
		// a physical dir before falling through to root.
		out = append(out, joinPath(majorV, licenseRelPath))
	}
	out = append(out, licenseRelPath)
	// Dedupe while preserving order.
	seen := map[string]bool{}
	uniq := out[:0]
	for _, p := range out {
		if !seen[p] {
			seen[p] = true
			uniq = append(uniq, p)
		}
	}
	return uniq
}

func joinPath(parts ...string) string {
	var nonEmpty []string
	for _, p := range parts {
		p = strings.Trim(p, "/")
		if p != "" {
			nonEmpty = append(nonEmpty, p)
		}
	}
	return strings.Join(nonEmpty, "/")
}

// urlExists HEAD-checks a URL and reports 200. Used to pick among candidate
// LICENSE blob paths. Caches by URL since many modules share repo structure.
var (
	urlExistsCache   = map[string]bool{}
	urlExistsCacheMu sync.Mutex
)

func urlExists(url string) bool {
	urlExistsCacheMu.Lock()
	if v, ok := urlExistsCache[url]; ok {
		urlExistsCacheMu.Unlock()
		return v
	}
	urlExistsCacheMu.Unlock()

	req, _ := http.NewRequest("HEAD", url, nil) //nolint:gosec // url is built from constant origin + sanitized path
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	ok := err == nil && resp != nil && resp.StatusCode == 200
	if resp != nil {
		_ = resp.Body.Close()
	}

	urlExistsCacheMu.Lock()
	urlExistsCache[url] = ok
	urlExistsCacheMu.Unlock()
	return ok
}

// longestCommonPath returns the longest path-component prefix shared by all
// paths. e.g. ["a/b/c", "a/b/d"] -> "a/b". For a single path, returns it.
func longestCommonPath(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	if len(paths) == 1 {
		return paths[0]
	}
	sort.Strings(paths)
	first, last := paths[0], paths[len(paths)-1]
	n := 0
	for n < len(first) && n < len(last) && first[n] == last[n] {
		n++
	}
	if n == len(first) && (n == len(last) || last[n] == '/') {
		// first is a complete path-component prefix of last.
		return first
	}
	common := first[:n]
	if idx := strings.LastIndex(common, "/"); idx >= 0 {
		return common[:idx]
	}
	return ""
}

// listPackages runs `go list -deps -json ./...` in dir. Returns flat package
// list. Excludes test packages.
func listPackages(dir string) ([]goListPackage, error) {
	cmd := exec.Command("go", "list", "-deps", "-json", "./...")
	cmd.Dir = dir
	cmd.Stderr = os.Stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(out)
	var pkgs []goListPackage
	for dec.More() {
		var p goListPackage
		if err := dec.Decode(&p); err != nil {
			return nil, err
		}
		pkgs = append(pkgs, p)
	}
	return pkgs, cmd.Wait()
}

func fetchInfo(m module) (*proxyInfo, error) {
	url := fmt.Sprintf("%s/%s/@v/%s.info", proxyBase, escapePath(m.Path), m.Version)
	resp, err := http.Get(url) //nolint:gosec // url is built from constant proxy base + sanitized module path
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	var info proxyInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return &info, nil
}

func fetchZip(m module) (*zip.Reader, error) {
	url := fmt.Sprintf("%s/%s/@v/%s.zip", proxyBase, escapePath(m.Path), m.Version)
	resp, err := http.Get(url) //nolint:gosec // url is built from constant proxy base + sanitized module path
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return zip.NewReader(bytes.NewReader(body), int64(len(body)))
}

// escapePath applies the Go module proxy's case-encoding rule: uppercase
// letters get encoded as "!<lower>" so that case-insensitive filesystems
// round-trip correctly. (See Go module proxy spec.)
func escapePath(p string) string {
	var b strings.Builder
	for _, r := range p {
		if r >= 'A' && r <= 'Z' {
			b.WriteByte('!')
			b.WriteRune(r + ('a' - 'A'))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// parseFallback reads a markdown table file (the existing third_party.md) and
// returns module-path -> URL for entries with a non-Unknown URL. Used when
// the proxy lacks Origin for an older module.
func parseFallback(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	rowRE := regexp.MustCompile(`^\|\s*([^|]+?)\s*\|\s*\[[^\]]*\]\(([^)]*)\)\s*\|\s*$`)
	out := map[string]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		m := rowRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name := strings.TrimSpace(m[1])
		url := strings.TrimSpace(m[2])
		if url == "" || strings.EqualFold(url, "unknown") {
			continue
		}
		// Reduce sub-package names back to the module path. We don't know
		// the actual module path from the table; treat the row name itself
		// as a candidate fallback key. nearestLicense semantics handle the
		// mismatch gracefully (we'll still try the module-path lookup; sub-
		// package paths will simply miss, which is fine).
		out[name] = url
	}
	return out, scanner.Err()
}

// multiFlag implements flag.Value for repeatable --ignore.
type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(s string) error { *m = append(*m, s); return nil }

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "licensereport: "+format+"\n", args...)
	os.Exit(1)
}
