// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package main

import "github.com/redpanda-data/redpanda-operator/harpoon/tablegenerator"

func main() {
	tablegenerator.RunGenerator("README.md", "<!-- insert snippet -->", "###", []string{"eks", "aks", "gke", "k3d"}...)
}
