// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package steps

import (
	"context"
	"fmt"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
)

func checkServiceWithPort(ctx context.Context, t framework.TestingT, serviceName, portName string, port int32) {
	var service corev1.Service

	key := t.ResourceKey(serviceName)

	t.Logf("Checking service %q has port %q with value %d", serviceName, portName, port)
	require.Eventually(t, func() bool {
		require.NoError(t, t.Get(ctx, key, &service))

		for _, servicePort := range service.Spec.Ports {
			if servicePort.Name == portName {
				hasMatchingPort := servicePort.Port == port
				t.Logf(`Checking port %q has value %d? %v (%d)`, portName, port, hasMatchingPort, servicePort.Port)
				return hasMatchingPort
			}
		}

		t.Logf(`Did not find port named %q`, portName)
		return false
	}, 5*time.Minute, 5*time.Second, "%s", delayLog(func() string {
		return fmt.Sprintf(`Service %q never contained port named %q with value %d`, key.String(), portName, port)
	}))
	t.Logf("Found port named %q on service %q with value %d!", portName, serviceName, port)
}
