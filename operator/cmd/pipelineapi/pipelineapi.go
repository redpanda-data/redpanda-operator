// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package pipelineapi provides the pipeline-api command.
package pipelineapi

import (
	"log/slog"

	"github.com/cockroachdb/errors"
	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	"github.com/redpanda-data/redpanda-operator/operator/internal/pipelineapi"
)

// Command returns the pipeline-api command, which serves Redpanda Connect's
// PipelineService from Pipeline resources for Redpanda Console to proxy to.
func Command() *cobra.Command {
	var (
		cfg                  pipelineapi.Config
		serve                pipelineapi.ServeOptions
		allowedCallers       []string
		audiences            []string
		allowUnauthenticated bool
	)

	cmd := &cobra.Command{
		Use:   "pipeline-api",
		Short: "Serve the Redpanda Connect pipeline API from Pipeline resources",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			logger := slog.New(logr.ToSlogHandler(ctrl.LoggerFrom(ctx).WithName("pipeline-api")))

			restConfig, err := ctrl.GetConfig()
			if err != nil {
				return errors.Wrap(err, "loading kubeconfig")
			}
			c, err := client.New(restConfig, client.Options{Scheme: controller.V2Scheme})
			if err != nil {
				return errors.Wrap(err, "creating kubernetes client")
			}

			svc, err := pipelineapi.NewService(cfg, c, logger)
			if err != nil {
				return err
			}
			auth, err := pipelineapi.NewAuthInterceptor(pipelineapi.AuthOptions{
				Reviewer:             &pipelineapi.KubernetesTokenReviewer{Client: c, Audiences: audiences},
				AllowedCallers:       allowedCallers,
				AllowUnauthenticated: allowUnauthenticated,
			})
			if err != nil {
				return err
			}
			if allowUnauthenticated {
				logger.Warn("authentication is disabled; anything that can reach the pipeline API can manage pipelines")
			}

			logger.Info("serving pipeline API", slog.String("address", serve.Address), slog.String("namespace", cfg.Namespace))
			return pipelineapi.Serve(ctx, pipelineapi.Handler(svc, auth), serve)
		},
	}

	cmd.Flags().StringVar(&cfg.Namespace, "namespace", "", "Namespace whose Pipelines the API serves. Required.")
	cmd.Flags().StringVar(&cfg.ClusterRef, "cluster-ref", "", "Redpanda resource every pipeline the API creates connects to.")
	cmd.Flags().StringVar(&cfg.UserRef, "user-ref", "", "User resource every pipeline the API creates authenticates as. Required with --cluster-ref.")
	cmd.Flags().StringVar(&serve.Address, "listen-address", ":8080", "Address to serve the API on.")
	cmd.Flags().StringVar(&serve.CertFile, "tls-cert-file", "", "Serve TLS with this certificate.")
	cmd.Flags().StringVar(&serve.KeyFile, "tls-key-file", "", "Serve TLS with this key.")
	cmd.Flags().StringSliceVar(&allowedCallers, "allowed-caller", nil, "Kubernetes username allowed to call the API, such as system:serviceaccount:<namespace>:<console>. Repeatable.")
	cmd.Flags().StringSliceVar(&audiences, "token-audience", []string{pipelineapi.DefaultAudience}, "Audience callers' bearer tokens must be issued for.")
	cmd.Flags().BoolVar(&allowUnauthenticated, "allow-unauthenticated", false, "Skip authentication. Local development only.")

	return cmd
}
