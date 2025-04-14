//go:build rewrites
// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

//nolint:all
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"example.com/example/aaacommon"
	"example.com/example/astrewrites"
	"example.com/example/changing_inputs"
	"example.com/example/directives"
	"example.com/example/files"
	"example.com/example/flowcontrol"
	"example.com/example/inputs"
	"example.com/example/k8s"
	"example.com/example/labels"
	"example.com/example/mutability"
	"example.com/example/sprig"
	"example.com/example/syntax"
	"example.com/example/typing"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
)

func main() {
	// Attempt to load a Kubernetes client config from KUBECONFIG. We ignore
	// any errors as failures will surface in a different way and doing so
	// preserves the ability to run this binary directly for debugging
	// purposes.
	var kubeConfig *kube.RESTConfig
	ctl, err := kube.FromEnv()
	if err == nil {
		kubeConfig = ctl.RestConfig()
	}

	enc := json.NewEncoder(os.Stdout)
	dec := json.NewDecoder(os.Stdin)

	for {
		var dot helmette.Dot
		err_1 := dec.Decode(&dot)
		if err_1 != nil {
			if err_1 == io.EOF {
				break
			}
			panic(err_1)
		}

		// HACK: Inject an FS into .Templates and Files to test `tpl` and
		// Files, respectively. This is done "lazily" so this FS always
		// contains freshly transpiled templates.
		dot.Templates = os.DirFS("./" + dot.Chart.Name)
		dot.Files = helmette.NewFiles(os.DirFS("./" + dot.Chart.Name))

		// HACK: Inject the kube rest client we've picked up from KUBECONFIG.
		dot.KubeConfig = kubeConfig

		out, err := runChart(&dot)

		if out == nil {
			out = map[string]any{}
		}

		if err != nil {
			err = fmt.Sprintf("%+v", err)
		}
		err_2 := enc.Encode(map[string]any{
			"result": out,
			"err":    err,
		})
		if err_2 !=
			nil {
			panic(err_2)
		}
	}
}

func runChart(dot *helmette.Dot) (_ map[string]any, err any) {
	defer func() {
		r_3 := recover()
		if r_3 != nil {
			err = fmt.Errorf("%v\n%s", r_3, debug.Stack())
		}
	}()

	switch dot.Chart.Name {
	case "aaacommon":
		return map[string]any{
			"SharedConstant": aaacommon.SharedConstant(),
		}, nil

	case "astrewrites":
		return map[string]any{
			"ASTRewrites": astrewrites.ASTRewrites(),
		}, nil

	case "labels":
		return map[string]any{
			"FullLabels": labels.FullLabels(dot),
		}, nil

	case "bootstrap":
		return map[string]any{}, nil

	case "sprig":
		return map[string]any{
			"Sprig": sprig.Sprig(dot),
		}, nil

	case "typing":
		return map[string]any{
			"Typing": typing.Typing(dot),
		}, nil

	case "directives":
		return map[string]any{
			"Directives": directives.Directives(),
		}, nil

	case "mutability":
		return map[string]any{
			"Mutability": mutability.Mutability(),
		}, nil

	case "k8s":
		return map[string]any{
			"K8s": k8s.K8s(dot),
		}, nil

	case "flowcontrol":
		return map[string]any{
			"FlowControl": flowcontrol.FlowControl(dot),
		}, nil

	case "inputs":
		return map[string]any{
			"Inputs": inputs.Inputs(dot),
		}, nil

	case "changing_inputs":
		return map[string]any{
			"ChangingInputs": changing_inputs.ChangingInputs(dot),
		}, nil

	case "syntax":
		return map[string]any{
			"Syntax": syntax.Syntax(),
		}, nil

	case "files":
		return map[string]any{
			"Files": files.Files(dot),
		}, nil

	default:
		panic(fmt.Sprintf("unknown package %q", dot.Chart.Name))
	}
}
