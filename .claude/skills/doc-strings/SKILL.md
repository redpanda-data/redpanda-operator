---
name: doc-strings
description: Writing standards for user-facing doc strings in this repo — Go doc comments on CRD fields (operator/api/redpanda/v1alpha2/) and helm-docs comments in chart values.yaml files. Apply when adding or editing those comments, or reviewing a diff that touches them. They are published documentation, generated verbatim into the CRD reference, kubectl explain output, Helm chart docs, and docs.redpanda.com.
---

# Doc strings

CRD field doc comments and `values.yaml` helm-docs comments in this repo are
not code comments. Generators publish them verbatim: `crd-ref-docs` and
`controller-gen` turn field comments into the CRD reference on
docs.redpanda.com AND the OpenAPI schema descriptions behind
`kubectl explain`; `helm-docs` turns `# --` comments into the chart README
and the Helm reference pages. Write them for the operator running the
cluster, not for the maintainer reading the Go.

The authoritative standard is `resources/writing-style/embedded-reference-strings.md`
in redpanda-data/docs-team-standards (private; fetch it, never vendor a
copy). The rules below are the local summary.

## CRD field comments (operator/api/redpanda/v1alpha2/)

- Describe the YAML key the user types (the `json:` tag), never the Go
  field name. "ClusterSource is a reference to..." documents the wrong
  identifier; the user writes `cluster:`.
- Enumerate legal values in prose even when a
  `+kubebuilder:validation:Enum` marker exists: the marker validates, the
  prose is what users read in `kubectl explain`.
- State defaults ("Defaults to ...") and what happens when the field is
  absent. `+kubebuilder:default` markers are stripped, not rendered.
- Every user-facing struct and field gets a comment. An undocumented
  struct ships as an empty reference table. `operator/crd-ref-docs-config.yaml`
  (`hidefromdoc`, ignoreTypes) defines what is user-facing.
- Marker lines (`+kubebuilder:...`, `+optional`, `+required`) are stripped
  by the generator; everything else in the comment block is published.

## Helm values comments (charts/*/chart/values.yaml)

- Document a key with `# -- description` immediately above it;
  continuation lines keep `#` without `--`.
- `# @default -- <text>` when the YAML default is empty or computed;
  `# @raw` for verbatim blocks; `# @ignored` to exclude.
- Never leave `# --` markers inside commented-out example blocks:
  helm-docs cannot attach them and the text silently disappears.
- Every user-visible key gets a description; cross-reference sibling keys
  with their full path (`listeners.<listener-name>.tls.cert`).

## Quality bar (both surfaces)

State the effect and when to change the setting; never restate the name;
give defaults and units; no internal jargon; describe current behavior,
not roadmap. After editing, run `task generate` so the generated docs
(README.md, CRD YAML) stay in sync — CI diffs them.

## Check published content

Changing a default, unit, or behavior, removing or renaming a surface, or
adding a new one can make published docs wrong or leave a gap. Search
docs.redpanda.com for the surface name before finalizing the change (the
public docs MCP at https://docs.redpanda.com/mcp exposes search and Q&A
tools; plain web search works too). If a published page states the old
behavior, say so in the PR description and update the doc string in the
same diff; the PR review automation routes high-impact cases to the docs
team's Jira intake (comments on the existing DOC ticket or files one).

## What NOT to flag in review

Subjective wording on a comment that already states effect, default, and
legal values; internal code comments; anything outside the two surfaces
above.
