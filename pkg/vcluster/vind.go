// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package vcluster

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/cockroachdb/errors"
	"golang.org/x/sys/unix"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

// VindCluster represents a vCluster in Docker (vind) cluster.
type VindCluster struct {
	name           string
	config         *rest.Config
	kubeconfigData []byte
}

// VindOpt configures a vind cluster.
type VindOpt func(*vindConfig)

type vindConfig struct {
	network        string
	podCIDR        string
	serviceCIDR    string
	installCertMgr bool
}

// VindWithNetwork sets the Docker network for the vind cluster.
func VindWithNetwork(network string) VindOpt {
	return func(c *vindConfig) { c.network = network }
}

// VindWithPodCIDR sets the pod CIDR for the vind cluster.
func VindWithPodCIDR(cidr string) VindOpt {
	return func(c *vindConfig) { c.podCIDR = cidr }
}

// VindWithServiceCIDR sets the service CIDR for the vind cluster.
func VindWithServiceCIDR(cidr string) VindOpt {
	return func(c *vindConfig) { c.serviceCIDR = cidr }
}

// VindWithCertManager deploys cert-manager inside the vind cluster via the
// experimental.deploy.vcluster.helm mechanism.
func VindWithCertManager() VindOpt {
	return func(c *vindConfig) { c.installCertMgr = true }
}

// CreateVind creates a new vind (vCluster in Docker) cluster.
// If a cluster with the same name already exists, it is upgraded.
// A file-based lock serializes creation across parallel test processes
// because the vind binary extraction is not concurrent-safe.
//
//nolint:gosec // this code is for tests
func CreateVind(name string, opts ...VindOpt) (*VindCluster, error) {
	cfg := &vindConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	// Use a file lock to coordinate vind cluster creation across parallel
	// test processes (go test -p=N). The vind binary extraction to
	// ~/.vcluster/docker/ races when multiple processes create concurrently.
	// A single global lock is used because the extraction path is shared.
	unlock, err := vindLockFile("create")
	if err != nil {
		return nil, errors.Wrap(err, "acquiring vind lock")
	}
	defer unlock()

	args := []string{
		"create", name,
		"--driver", "docker",
		"--connect=false",
		"--upgrade",
	}

	valuesYAML := buildVindValues(cfg)
	if valuesYAML != "" {
		f, err := os.CreateTemp("", "vind-values-*.yaml")
		if err != nil {
			return nil, errors.Wrap(err, "creating temp values file")
		}
		defer os.Remove(f.Name())
		if _, err := f.WriteString(valuesYAML); err != nil {
			return nil, errors.Wrap(err, "writing values file")
		}
		f.Close()
		args = append(args, "-f", f.Name())
	}

	out, err := exec.Command("vcluster", args...).CombinedOutput()
	if err != nil {
		return nil, errors.Wrapf(err, "vcluster create %s: %s", name, out)
	}

	kubeconfigData, config, err := vindKubeconfig(name)
	if err != nil {
		return nil, err
	}

	return &VindCluster{name: name, config: config, kubeconfigData: kubeconfigData}, nil
}

// vindLockFile acquires an exclusive file lock to coordinate vind operations
// across parallel test processes.
//
//nolint:gosec // best-effort cleanup in test infrastructure
func vindLockFile(name string) (func(), error) {
	lockPath := filepath.Join(os.TempDir(), "vind-"+name+".lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		f.Close() //nolint:errcheck
		return nil, err
	}
	return func() {
		_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
		_ = f.Close()
	}, nil
}

//nolint:gosec // this code is for tests
func vindKubeconfig(name string) ([]byte, *rest.Config, error) {
	out, err := exec.Command(
		"vcluster", "connect",
		"--silent", "--print", "--driver=docker",
		name,
	).CombinedOutput()
	if err != nil {
		return nil, nil, errors.Wrapf(err, "vcluster connect --print %s: %s", name, out)
	}

	config, err := clientcmd.RESTConfigFromKubeConfig(out)
	if err != nil {
		return nil, nil, errors.Wrap(err, "parsing vind kubeconfig")
	}

	return out, config, nil
}

// Name returns the cluster name.
func (v *VindCluster) Name() string { return v.name }

// RESTConfig returns a copy of the cluster's REST config.
func (v *VindCluster) RESTConfig() *rest.Config { return rest.CopyConfig(v.config) }

// WriteKubeconfig writes the raw kubeconfig to the given path. This is useful
// for tools (like the helm CLI) that need a kubeconfig file rather than a
// rest.Config, since the kube.RestToConfig round-trip loses fields like
// insecure-skip-tls-verify.
func (v *VindCluster) WriteKubeconfig(path string) error {
	return os.WriteFile(path, v.kubeconfigData, 0o600)
}

// Delete removes the vind cluster.
//
//nolint:gosec // this code is for tests
func (v *VindCluster) Delete() error {
	out, err := exec.Command("vcluster", "delete", v.name, "--driver", "docker").CombinedOutput()
	if err != nil {
		return errors.Wrapf(err, "vcluster delete %s: %s", v.name, out)
	}
	return nil
}

// Containers returns all Docker container names for this vind cluster
// (control plane + worker nodes).
//
//nolint:gosec // this code is for tests
func (v *VindCluster) Containers() ([]string, error) {
	out, err := exec.Command("docker", "ps", "--format", "{{.Names}}").CombinedOutput()
	if err != nil {
		return nil, errors.Wrapf(err, "listing containers: %s", out)
	}
	cpName := "vcluster.cp." + v.name
	nodePrefix := "vcluster.node." + v.name + "."
	var result []string
	for _, name := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if name == cpName || strings.HasPrefix(name, nodePrefix) {
			result = append(result, name)
		}
	}
	return result, nil
}

// CPContainerIP returns the Docker-internal IP of the control plane container.
//
//nolint:gosec // this code is for tests
func (v *VindCluster) CPContainerIP() (string, error) {
	container := "vcluster.cp." + v.name
	out, err := exec.Command(
		"docker", "inspect", "-f",
		"{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}",
		container,
	).Output()
	if err != nil {
		return "", errors.Wrapf(err, "inspecting container %s", container)
	}
	ip := strings.TrimSpace(string(out))
	if ip == "" {
		return "", errors.Newf("no IP found for container %s", container)
	}
	return ip, nil
}

var vindValuesTemplate = template.Must(template.New("vind-values").Parse(`
{{- if or .Network .InstallCertMgr }}
experimental:
  {{- if .Network }}
  docker:
    network: {{ .Network }}
  {{- end }}
  {{- if .InstallCertMgr }}
  deploy:
    vcluster:
      helm:
      - chart:
          name: cert-manager
          repo: https://charts.jetstack.io
          version: {{ .CertManagerVersion }}
        values: |
          installCRDs: true
          global:
            leaderElection:
              renewDeadline: 10s
              retryPeriod: 5s
        release:
          name: cert-manager
          namespace: cert-manager
  {{- end }}
{{- end }}
{{- if or .PodCIDR .ServiceCIDR }}
networking:
  {{- if .PodCIDR }}
  podCIDR: {{ .PodCIDR }}
  {{- end }}
  {{- if .ServiceCIDR }}
  serviceCIDR: {{ .ServiceCIDR }}
  {{- end }}
{{- end }}
`))

func buildVindValues(cfg *vindConfig) string {
	var buf bytes.Buffer
	_ = vindValuesTemplate.Execute(&buf, struct {
		Network            string
		PodCIDR            string
		ServiceCIDR        string
		InstallCertMgr     bool
		CertManagerVersion string
	}{
		Network:            cfg.network,
		PodCIDR:            cfg.podCIDR,
		ServiceCIDR:        cfg.serviceCIDR,
		InstallCertMgr:     cfg.installCertMgr,
		CertManagerVersion: testutil.CertManagerVersion,
	})
	return buf.String()
}
