// This module is deliberately not listed in the repo's go.work: it exists to
// lint the workspace, not to be part of it, and keeping it out means its
// dependencies (x/tools and the linters below) cannot shift the versions the
// shipped modules build against. Build it with GOWORK=off; see README.md.
module github.com/redpanda-data/redpanda-operator/lint

go 1.26.0

require (
	github.com/OpenPeeDeeP/depguard/v2 v2.2.1
	github.com/daixiang0/gci v0.13.7
	github.com/golangci/misspell v0.8.0
	github.com/gordonklaus/ineffassign v0.2.0
	github.com/hidalgopl/laconiccomments v0.2.0
	github.com/julz/importas v0.2.0
	github.com/securego/gosec/v2 v2.28.0
	golang.org/x/tools v0.49.0
	honnef.co/go/tools v0.8.1
	mvdan.cc/gofumpt v0.11.0
	mvdan.cc/unparam v0.0.0-20260823230713-2fa3d841b0c8
	sigs.k8s.io/yaml v1.6.0
)

require (
	github.com/BurntSushi/toml v1.6.0 // indirect
	github.com/ccojocar/zxcvbn-go v1.0.4 // indirect
	github.com/gobwas/glob v0.2.3 // indirect
	github.com/hexops/gotextdiff v1.0.3 // indirect
	go.uber.org/atomic v1.7.0 // indirect
	go.uber.org/multierr v1.6.0 // indirect
	go.uber.org/zap v1.24.0 // indirect
	go.yaml.in/yaml/v2 v2.4.2 // indirect
	golang.org/x/exp/typeparams v0.0.0-20231108232855-2478ac86f678 // indirect
	golang.org/x/mod v0.39.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
