// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

//go:build install

package main

import (
	_ "embed"
	"fmt"

	"github.com/redpanda-data/redpanda/src/go/rpk/pkg/out"
	"github.com/redpanda-data/redpanda/src/go/rpk/pkg/plugin"
	"github.com/spf13/afero"
)

//go:embed plugin
var bin []byte

func main() {
	fs := afero.NewOsFs()

	pluginDir, err := plugin.DefaultBinPath()
	out.MaybeDieErr(err)

	if exists, _ := afero.DirExists(fs, pluginDir); !exists {
		out.MaybeDieErr(fs.MkdirAll(pluginDir, 0o755))
	}

	path, err := plugin.WriteBinary(fs, "k8s", pluginDir, bin, false, false)
	out.MaybeDieErr(err)
	fmt.Println("installed at path", path)
}
