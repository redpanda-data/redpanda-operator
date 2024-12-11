// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"sigs.k8s.io/yaml"

	"github.com/redpanda-data/redpanda-operator/charts/operator"
)

func main() {
	metadata := operator.Chart.Metadata()
	metadata.Name = "redpanda-operator-nightly"
	// TAG_NAME is created in buildkite pipeline and `ci/scripts/tag-check.sh`
	metadata.Version = fmt.Sprintf("%s-helm-chart", os.Getenv("TAG_NAME"))
	b, err := yaml.Marshal(metadata)
	dieOnError(err)

	pwd, err := os.Getwd()
	dieOnError(err)

	err = os.WriteFile(filepath.Join(pwd, "charts/operator/Chart.yaml"), b, os.ModePerm)
	dieOnError(err)
}

func dieOnError(err error) {
	if err != nil {
		log.Printf("error processing files: %v", err)
		os.Exit(1)
	}
}
