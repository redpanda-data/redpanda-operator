// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package ready

import (
	"context"
	"errors"
	"fmt"

	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/redpanda-data/redpanda-operator/operator/internal/probes"
	"github.com/spf13/cobra"
)

func Command() *cobra.Command {
	var (
		redpandaYAMLPath string
		brokerID         int
	)

	cmd := &cobra.Command{
		Use:   "ready",
		Short: "Run the redpanda broker readiness probe and exit",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			return Run(
				ctx,
				redpandaYAMLPath,
				brokerID,
			)
		},
	}

	cmd.Flags().StringVar(&redpandaYAMLPath, "redpanda-yaml", "/etc/redpanda/redpanda.yaml", "Path to redpanda.yaml whose rpk stanza will be used for connecting to a Redpanda cluster.")
	cmd.Flags().IntVar(&brokerID, "broker-id", -1, "The ID of the broker instance this readiness check is for.")

	return cmd
}

func Run(
	ctx context.Context,
	redpandaYAMLPath string,
	brokerID int,
) error {
	logger := ctrl.LoggerFrom(ctx)

	if brokerID < 0 {
		err := fmt.Errorf("invalid broker id: %d", brokerID)
		logger.Error(err, "must specify a broker via the -broker-id flag")
		return err
	}

	prober := probes.NewProber(
		internalclient.NewRPKOnlyFactory(),
		redpandaYAMLPath,
		probes.WithLogger(logger),
	)

	ready, err := prober.IsClusterBrokerReady(ctx, brokerID)
	if err != nil {
		logger.Error(err, "error checking broker readiness")
		return err
	}

	if !ready {
		return errors.New("broker not ready")
	}

	return nil
}
