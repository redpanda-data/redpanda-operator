// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package status normalizes the way CRD statuses are handled within our
// code base. The statuses are generated and documented via modifying "statuses.yaml".
// Each status is meant to fill in the conditions array of a Kubernetes CRD with
// some sort of uniform structure. The structures are used via setting errors on any
// non-true condition of the overall structure and subsequently calling `Conditions(generation)`
// on the parent CR. Here's an example:
//
//	var status statuses.ClusterStatus
//	var cluster redpandav1alpha2.Redpanda
//
//	cluster.SetGeneration(1)
//	status.Healthy.NotHealthy = errors.New("cluster isn't healthy")
//	status.Stable.Unstable = errors.New("unstable for these reasons")
//
//	conditions := status.Conditions(cluster.GetGeneration())
//
// The above results in something that looks like this in JSON:
//
//	[{
//	  "type": "Ready",
//	  "status": "True",
//	  "observedGeneration": 1,
//	  "reason": "Ready",
//	  "message": "Cluster ready to service requests"
//	},
//	{
//	  "type": "Healthy",
//	  "status": "False",
//	  "observedGeneration": 1,
//	  "reason": "NotHealthy",
//	  "message": "cluster isn't healthy"
//	},
//	{
//	  "type": "ClusterConfigurationApplied",
//	  "status": "True",
//	  "observedGeneration": 1,
//	  "reason": "Applied",
//	  "message": "Cluster configuration successfully applied"
//	},
//	{
//	  "type": "LicenseValid",
//	  "status": "True",
//	  "observedGeneration": 1,
//	  "reason": "LicenseValid",
//	  "message": "Valid"
//	},
//	{
//	  "type": "Quiesced",
//	  "status": "True",
//	  "observedGeneration": 1,
//	  "reason": "Quiesced",
//	  "message": "Cluster reconciliation finished"
//	},
//	{
//	  "type": "Stable",
//	  "status": "False",
//	  "observedGeneration": 1,
//	  "reason": "Unstable",
//	  "message": "unstable for these reasons"
//	}]
package status
