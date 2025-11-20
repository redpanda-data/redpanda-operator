// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package multicluster

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/andrewstucki/locking/multicluster/bootstrap"
	"github.com/spf13/cobra"
)

type RemoteConfiguration struct {
	ContextName    string
	ServiceAddress string
}

type RotateCertificatesOptions struct {
	OperatorNamespace string
	ServiceName       string
	RemoteClusters    []RemoteConfiguration

	remoteClusterStrings []string
}

func (o *RotateCertificatesOptions) BindFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&o.OperatorNamespace, "operator-namespace", "redpanda-system", "namespace in which the operator will be deployed")
	cmd.Flags().StringVar(&o.ServiceName, "operator-name", "redpanda-multicluster-operator", "name of the operator being deployed")
	cmd.Flags().StringSliceVar(&o.remoteClusterStrings, "cluster", []string{}, "cluster context + either DNS or IP where other operators will communicate with the operator deployed in the context")
}

func (o *RotateCertificatesOptions) Validate() error {
	if len(o.remoteClusterStrings) < 3 {
		return errors.New("must specify at least 3 remote clusters")
	}

	for _, addr := range o.remoteClusterStrings {
		parsed, err := url.Parse(addr)
		if err != nil {
			return errors.New("invalid cluster flag, must be of the form context-name://address")
		}

		if parsed.Scheme == "" || parsed.Host == "" {
			return errors.New("invalid cluster flag, must be of the form context-name://address")
		}

		o.RemoteClusters = append(o.RemoteClusters, RemoteConfiguration{
			ContextName:    parsed.Scheme,
			ServiceAddress: parsed.Host,
		})
	}

	return nil
}

func RotateCertificatesCommand(multiclusterOptions *MulticlusterOptions) *cobra.Command {
	var options RotateCertificatesOptions

	cmd := &cobra.Command{
		Use: "rotate-certificates",
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunRotateCertificates(cmd.Context(), &options, multiclusterOptions)
		},
	}

	options.BindFlags(cmd)

	return cmd
}

func RunRotateCertificates(
	ctx context.Context,
	opts *RotateCertificatesOptions,
	multiclusterOpts *MulticlusterOptions,
) error {
	if err := multiclusterOpts.Validate(); err != nil {
		return err
	}

	if err := opts.Validate(); err != nil {
		return err
	}

	config := bootstrap.BootstrapClusterConfiguration{
		BootstrapTLS:      true,
		EnsureNamespace:   true,
		OperatorNamespace: opts.OperatorNamespace,
		ServiceName:       opts.ServiceName,
	}

	contextNames := []string{}
	for _, cluster := range opts.RemoteClusters {
		contextNames = append(contextNames, cluster.ContextName)
		config.RemoteClusters = append(config.RemoteClusters, bootstrap.RemoteConfiguration{
			ContextName:    cluster.ContextName,
			ServiceAddress: cluster.ServiceAddress,
		})
	}

	if err := bootstrap.BootstrapKubernetesClusters(ctx, "redpanda-multicluster-operator", config); err != nil {
		return err
	}

	fmt.Printf("Rotated certificates for: %s\n", strings.Join(contextNames, ", "))
	return nil
}
