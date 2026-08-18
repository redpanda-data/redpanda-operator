// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package multicluster

import (
	"os"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// LoadKubeconfig reads a kubeconfig file from disk and returns its
// CurrentContext as a *rest.Config.
func LoadKubeconfig(file string) (*rest.Config, error) {
	kubeconfigYAML, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}

	return LoadKubeconfigFromBytes(kubeconfigYAML)
}

// LoadKubeconfigFromBytes parses kubeconfig YAML bytes and returns the
// CurrentContext as a *rest.Config. Used both by the operator's own
// raft-bootstrap flow (where the bytes come from a cached Secret) and by
// external tools that read those Secrets directly.
func LoadKubeconfigFromBytes(kubeconfigYAML []byte) (*rest.Config, error) {
	kubeconfig, err := clientcmd.Load(kubeconfigYAML)
	if err != nil {
		return nil, err
	}

	clientConfig := clientcmd.NewNonInteractiveClientConfig(*kubeconfig, kubeconfig.CurrentContext, &clientcmd.ConfigOverrides{}, nil)
	return clientConfig.ClientConfig()
}
