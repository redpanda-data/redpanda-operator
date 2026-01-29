// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package migration contains a post-upgrade job that handles any
// sort of migrations needed for an upgrade.
package migration

import (
	"context"
	"fmt"
	"log"

	"github.com/redpanda-data/common-go/kube"
	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	"github.com/redpanda-data/redpanda-operator/operator/internal/lifecycle"
)

func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migration",
		Short: "Run migrations for Redpanda Operator",
		Run: func(cmd *cobra.Command, args []string) {
			ctx := cmd.Context()

			run(
				ctx,
			)
		},
	}

	return cmd
}

func run(
	ctx context.Context,
) {
	log.Printf("Running migrations for Redpanda Operator")

	scheme := controller.UnifiedScheme
	config := ctrl.GetConfigOrDie()
	k8sClient, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		log.Fatalf("%s", fmt.Errorf("unable to run migrations: %w", err))
	}

	ctl, err := kube.FromRESTConfig(config, kube.Options{
		Options: client.Options{
			Scheme: scheme,
		},
		FieldManager: string(lifecycle.DefaultFieldOwner),
	})
	if err != nil {
		log.Fatalf("%s", fmt.Errorf("unable to create kube ctl: %w", err))
	}

	if err := migrateFieldManagers(ctx, ctl, k8sClient); err != nil {
		log.Fatalf("%s", fmt.Errorf("unable to update field managers: %w", err))
	}
}
