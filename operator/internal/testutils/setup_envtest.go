// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package testutils

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	crds "github.com/redpanda-data/redpanda-operator/operator/config/crd/bases"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// /internal/testutils is a depth of 2
const testutilsRelDepth = 2

type RedpandaTestEnv struct {
	envtest.Environment
}

func (e *RedpandaTestEnv) StartRedpandaTestEnv(withWebhook bool) (*rest.Config, error) {
	configPath, err := configRelpath()
	if err != nil {
		return nil, fmt.Errorf("unable to lookup path of calling function: %w", err)
	}

	e.CRDInstallOptions.CRDs = crds.All()

	if withWebhook {
		e.WebhookInstallOptions = envtest.WebhookInstallOptions{
			Paths: []string{filepath.Join(configPath, "webhook")},
		}
	}
	cfg, err := e.Start()
	return cfg, err
}

// because the relative path will be different depending on which test is running, this function
// determines the relative depth of the test to build the correct relative path to the config
// directory
func configRelpath() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("unable to lookup path of calling function: %w", errors.ErrUnsupported)
	}
	p := path.Dir(file)
	testutilsDepth := strings.Count(p, "/") - testutilsRelDepth
	_, file, _, ok = runtime.Caller(2)
	if !ok {
		return "", fmt.Errorf("unable to lookup path of calling function: %w", errors.ErrUnsupported)
	}
	p = path.Dir(file)
	c := strings.Count(p, "/")
	relpath := []string{}
	for i := testutilsDepth; i < c; i++ {
		relpath = append(relpath, "..")
	}
	relpath = append(relpath, "config")

	p = path.Join(relpath...)
	return p, nil
}
