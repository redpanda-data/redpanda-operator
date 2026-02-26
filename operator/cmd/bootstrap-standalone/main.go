// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/spf13/cobra"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"

	"github.com/redpanda-data/redpanda-operator/pkg/multicluster/bootstrap"
)

func main() {
	var (
		organization      string
		operatorNamespace string
		serviceName       string
		kubeconfigPath    string
	)
	rootCmd := &cobra.Command{
		Use:   "multicluster-bootstrap",
		Short: "Standalone multicluster bootstrap command",
		Run: func(cmd *cobra.Command, args []string) {
			ctx := cmd.Context()

			run(
				ctx,
				organization,
				operatorNamespace,
				serviceName,
				kubeconfigPath,
			)
		},
	}

	rootCmd.Flags().StringVar(&organization, "organization", "", "")
	rootCmd.Flags().StringVar(&operatorNamespace, "operatorNamespace", "", "")
	rootCmd.Flags().StringVar(&serviceName, "serviceName", "redpanda-operator-multicluster", "")
	rootCmd.Flags().StringVar(&kubeconfigPath, "kubeconfigPath", "", "")

	if err := rootCmd.Execute(); err != nil {
		fmt.Printf("%+v\n", err)
		os.Exit(1)
	}
}

// ClusterInfo represents a parsed cluster configuration from kubeconfig
type ClusterInfo struct {
	ContextName string
	ClusterName string
	UserName    string
	Config      *rest.Config
}

// ParseKubeConfig parses a kubeconfig file and returns an array of cluster configurations
func ParseKubeConfig(kubeconfigPath string) ([]ClusterInfo, error) {
	// Load the kubeconfig file
	config, err := clientcmd.LoadFromFile(kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig from %s: %w", kubeconfigPath, err)
	}

	var clusterInfos []ClusterInfo

	// Iterate through all contexts in the kubeconfig
	for contextName, context := range config.Contexts {
		// Create a rest.Config for this specific context
		restConfig, err := buildConfigFromContext(config, contextName)
		if err != nil {
			log.Printf("Warning: failed to build config for context %s: %v", contextName, err)
			continue
		}

		clusterInfos = append(clusterInfos, ClusterInfo{
			ContextName: contextName,
			ClusterName: context.Cluster,
			UserName:    context.AuthInfo,
			Config:      restConfig,
		})
	}

	if len(clusterInfos) == 0 {
		return nil, fmt.Errorf("no valid contexts found in kubeconfig")
	}

	return clusterInfos, nil
}

// buildConfigFromContext creates a rest.Config from a specific context in the kubeconfig
func buildConfigFromContext(config *api.Config, contextName string) (*rest.Config, error) {
	// Create a clientcmd.ClientConfig using the specific context
	configOverrides := &clientcmd.ConfigOverrides{
		CurrentContext: contextName,
	}

	clientConfig := clientcmd.NewNonInteractiveClientConfig(
		*config,
		contextName,
		configOverrides,
		nil,
	)

	// Build the rest.Config
	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to create rest config: %w", err)
	}

	return restConfig, nil
}

func run(
	ctx context.Context,
	organization string,
	operatorNamespace string,
	serviceName string,
	kubeconfigPath string,
) {
	log.Println("Creating certificates")

	remoteClusters := []bootstrap.RemoteConfiguration{}

	// Parse kubeconfig file if provided
	if kubeconfigPath == "" {
		log.Fatalf("kubeconfigPath is empty")
	}

	clusterInfos, err := ParseKubeConfig(kubeconfigPath)
	if err != nil {
		log.Fatalf("Failed to parse kubeconfig: %v", err)
	}

	// Convert ClusterInfo to RemoteConfiguration
	for _, clusterInfo := range clusterInfos {
		log.Printf("Adding cluster from context: %s (cluster: %s, user: %s)",
			clusterInfo.ContextName, clusterInfo.ClusterName, clusterInfo.UserName)

		remoteClusters = append(remoteClusters, bootstrap.RemoteConfiguration{
			KubeConfig:  clusterInfo.Config,
			ContextName: clusterInfo.ContextName,
		})
	}

	err = bootstrap.BootstrapKubernetesClusters(ctx, organization, bootstrap.BootstrapClusterConfiguration{
		BootstrapTLS:         true,
		BootstrapKubeconfigs: true,
		EnsureNamespace:      true,
		OperatorNamespace:    operatorNamespace,
		ServiceName:          serviceName,
		RemoteClusters:       remoteClusters,
	})
	if err != nil {
		log.Fatalf("%s", fmt.Errorf("unable to bootstrap multi cluster: %w", err))
	}

	log.Println("Certificates created")
}
