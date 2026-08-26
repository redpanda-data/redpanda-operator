// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package redpanda

import (
	"context"

	"github.com/cockroachdb/errors"
	"github.com/go-logr/logr"
	"github.com/redpanda-data/common-go/otelutil/log"

	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/schemaregistry"
)

// schemaRegistryReady is the roll-gate decision shared by the
// single-cluster and stretch reconcilers: fetch per-broker Schema Registry
// clients for the given cluster object and require every broker's SR store
// to be confirmed synced (schemaregistry.Synced).
//
// A cluster whose SR listener is disabled (schemaregistry.ErrDisabled)
// satisfies the gate trivially — there is nothing to protect. Every other
// failure — client construction included — reports false (fail closed) with
// the reason logged at debug; the caller defers the roll and re-probes on
// the next pass.
func schemaRegistryReady(ctx context.Context, factory internalclient.ClientFactory, cluster any, logger logr.Logger) bool {
	brokers, err := factory.SchemaRegistryBrokerClients(ctx, cluster)
	if errors.Is(err, schemaregistry.ErrDisabled) {
		logger.V(log.DebugLevel).Info("schema registry disabled on cluster, skipping schema registry sync roll gate")
		return true
	}
	if err != nil {
		logger.V(log.DebugLevel).Info("schema registry client init error, deferring rolling restart", "error", err.Error())
		return false
	}
	synced, err := schemaregistry.Synced(ctx, brokers, logger)
	if !synced {
		logArgs := []any{}
		if err != nil {
			logArgs = append(logArgs, "error", err.Error())
		}
		logger.V(log.DebugLevel).Info("a broker's schema registry store is still catching up, deferring rolling restart", logArgs...)
	}
	return synced
}
