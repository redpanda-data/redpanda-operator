// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package testing

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type KubectlOptions struct {
	ConfigPath string
	Namespace  string
	Env        map[string]string
}

func (o *KubectlOptions) RestConfig() (*rest.Config, error) {
	return restConfig(o)
}

func NewKubectlOptions(config string) *KubectlOptions {
	return &KubectlOptions{
		ConfigPath: config,
		Namespace:  metav1.NamespaceDefault,
		Env:        make(map[string]string),
	}
}

func (o *KubectlOptions) Clone() *KubectlOptions {
	opts := &KubectlOptions{
		ConfigPath: o.ConfigPath,
		Namespace:  o.Namespace,
		Env:        make(map[string]string),
	}

	for k, v := range o.Env {
		opts.Env[k] = v
	}

	return opts
}

func (o *KubectlOptions) args(args []string) []string {
	var cmdArgs []string
	if o.ConfigPath != "" {
		cmdArgs = append(cmdArgs, "--kubeconfig", o.ConfigPath)
	}
	if o.Namespace != "" && !slices.Contains(args, "-n") && !slices.Contains(args, "--namespace") {
		cmdArgs = append(cmdArgs, "--namespace", o.Namespace)
	}

	return append(cmdArgs, args...)
}

func (o *KubectlOptions) environment() []string {
	env := os.Environ()
	for key, value := range o.Env {
		env = append(env, fmt.Sprintf("%s=%s", key, value))
	}
	return env
}

func (o *KubectlOptions) merge(options *KubectlOptions) *KubectlOptions {
	if options.ConfigPath != "" {
		o.ConfigPath = options.ConfigPath
	}
	if options.Namespace != "" {
		o.Namespace = options.Namespace
	}
	for k, v := range options.Env {
		o.Env[k] = v
	}
	return o
}

func defaultOptions() *KubectlOptions {
	return &KubectlOptions{
		ConfigPath: clientcmd.RecommendedHomeFile,
		Namespace:  metav1.NamespaceDefault,
		Env:        make(map[string]string),
	}
}

func KubectlDelete(ctx context.Context, fileOrDirectory string, options ...*KubectlOptions) (string, error) {
	mergedOptions := defaultOptions()
	for _, option := range options {
		mergedOptions = mergedOptions.merge(option)
	}

	return kubectl(ctx, mergedOptions, "delete", "-f", fileOrDirectory, "--ignore-not-found=true")
}

func KubectlApply(ctx context.Context, fileOrDirectory string, options ...*KubectlOptions) (string, error) {
	mergedOptions := defaultOptions()
	for _, option := range options {
		mergedOptions = mergedOptions.merge(option)
	}

	return kubectl(ctx, mergedOptions, "apply", "--server-side", "-f", fileOrDirectory)
}

func kubectl(ctx context.Context, options *KubectlOptions, args ...string) (string, error) {
	command := exec.Command("kubectl", options.args(args)...) //nolint:gosec // This is just test code.
	command.Env = options.environment()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		// signal a cancel on the command to make
		// it responsive to upstream context cancelation
		<-ctx.Done()
		if command != nil && command.ProcessState != nil && !command.ProcessState.Exited() {
			_ = command.Cancel()
		}
	}()

	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %w", string(output), err)
	}

	return string(output), nil
}
