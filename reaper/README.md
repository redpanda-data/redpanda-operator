# (envtest) Reaper

A shim that wraps the `envtest` binaries (`etcd`, `kube-apiserver`) so they don't
outlive the test process that started them.

`reaper` is symlinked as `shims/etcd` and `shims/kube-apiserver` (see
`packages.envtest-shim` in `flake.nix`) and pointed at by `TEST_ASSET_ETCD` /
`TEST_ASSET_KUBE_APISERVER`. When run, it execs the identically named binary
from `$KUBEBUILDER_ASSETS`, forwarding stdio and its own exit code, and kills
that child once its own parent exits. Without it, a `go test` run that dies
without cleaning up (`SIGKILL`, a panic in the wrong place, `^C` in some
harnesses) leaves `etcd` and `kube-apiserver` processes behind.

Watching the parent is OS specific, so each `watch_$GOOS.go` implements
`watchParent`. Both block, without polling, until the parent exits or the
context is canceled:

- darwin: `kqueue` with `EVFILT_PROC` / `NOTE_EXIT`.
- linux: `pidfd_open` (Linux 5.3+), which yields an fd that becomes readable
  when the process exits, plus an `eventfd` to break out of `poll` on
  cancellation.

Note that `PR_SET_PDEATHSIG` is deliberately not used on linux: its notion of
"parent" is the *thread* that created the process, and Go's runtime retires
threads at will, which would kill the child while the test is still running.
