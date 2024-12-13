// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package supervisor

import (
	"context"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/redpanda-data/redpanda-operator/operator/internal/supervisor"
)

var schemes = []func(s *runtime.Scheme) error{
	clientgoscheme.AddToScheme,
}

func Command() *cobra.Command {
	var (
		rpkPath          string
		redpandaYAMLPath string
		shutdownTimeout  time.Duration
	)

	cmd := &cobra.Command{
		Use:   "supervisor",
		Short: "Run the redpanda supervisor",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			return Run(
				ctx,
				rpkPath,
				redpandaYAMLPath,
				shutdownTimeout,
				args...,
			)
		},
	}

	cmd.Flags().StringVar(&rpkPath, "rpk-path", "/usr/bin/rpk", "Path to rpk binary.")
	cmd.Flags().StringVar(&redpandaYAMLPath, "redpanda-yaml", "/etc/redpanda/redpanda.yaml", "Path to redpanda.yaml whose rpk stanza will be used for connecting to a Redpanda cluster.")
	cmd.Flags().DurationVar(&shutdownTimeout, "shutdown-timeout", 5*time.Second, "Wait time prior to killing the redpanda process when gracefully shutting down.")

	return cmd
}

func Run(
	ctx context.Context,
	rpkPath string,
	redpandaYAMLPath string,
	shutdownTimeout time.Duration,
	extraArgs ...string,
) error {
	logger := ctrl.LoggerFrom(ctx)
	setupLog := logger.WithName("setup")

	supervisor, err := supervisor.New(supervisor.Config{
		ExecutablePath:   rpkPath,
		RedpandaYamlPath: redpandaYAMLPath,
		Arguments:        extraArgs,
		ShutdownTimeout:  shutdownTimeout,
		Logger:           logger.WithName("Supervisor"),
	})
	if err != nil {
		setupLog.Error(err, "setting up supervisor")
		return err
	}

	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	return supervisor.Run(ctx)
}
