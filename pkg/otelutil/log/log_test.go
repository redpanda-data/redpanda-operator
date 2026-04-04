// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package log_test

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	"k8s.io/klog/v2"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/redpanda-data/redpanda-operator/pkg/otelutil/log"
)

func TestSetGlobals(t *testing.T) {
	var buf bytes.Buffer
	logger := logr.FromSlogHandler(slog.NewTextHandler(&buf, &slog.HandlerOptions{}))

	log.SetGlobals(logger)

	klog.Info("Hello from Klog")
	ctrllog.Log.Info("Hello from controller-runtime")
	slog.Info("Hello from slog")
	log.Info(context.TODO(), "Hello from pkg/otelutil/log")

	t.Logf("%s", buf.String())

	require.Contains(t, buf.String(), "Hello from Klog")
	require.Contains(t, buf.String(), "Hello from controller-runtime")
	require.Contains(t, buf.String(), "Hello from slog")
	require.Contains(t, buf.String(), "Hello from pkg/otelutil/log")
}
