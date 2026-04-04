# License Updater

This project allows for automatic addition and updating of license headers in a project, specifically keeping license years in sync.

## Installation

```bash
➜  go install github.com/redpanda-data/redpanda-operator/licenseupdater@latest
```

## Configuration

Below is an example configuration:

```yaml
top_level_license: MIT # the top level LICENSE file type, optional if a licenses array is supplied
licenses: [BSL, RCL] # the license files that are added to a licenses folder in markdown format, optional if top_level_license is supplied
files:
  - name: some/static/file.go.txt # this is a "static file" that is generated with only the license header in it, like matching rules, you can also specify a type or delimiter field to add some additional preprocessing of the license header before it's written to disk
    license: BSL
ignore:
    - directory: some/directory # specifies the directory that a file must be a child of to match, this can also be used in the matching rules below
    - extension: .someextension
      match: some.*regex # this specifies a regular expression used in matching, it can also be used in the matching rules below
    - name: some/filename/here.txt # specifies the exact file that matches this rule, this can also be used in the matching rules below
matches:
  - extension: .yaml # the main matching rule, this and the directory rule below will be AND'd together
    directory: some/directory # only match files that have this directory in their hierarchy
    type: helm # specifies that files matching this rule should use the built-in helm delimiter pattern for their header comment
    license: RCL # specifies which license to use
  - extension: .yaml
    directory: some/directory
    type: yaml
    license: RCL
  - extension: .go
    type: go
    license: BSL
  - extension: .txt
    delimiter: # defines a custom delimiter pattern to use
      top: !!!
      middle: !!!
      bottom: !!!
    license: BSL
```

## Running

```bash
➜  licenseupdater -config path/to/config.yaml # defaults to .licenseupdater.yaml in the current directory
```