// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package bootstrap

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	flag "github.com/spf13/pflag"
)

var (
	cliBootstrapClusterConfiguration BootstrapClusterConfiguration
	cliContexts                      []string
	cliServiceAddressOverrides       []string
	cliAPIServerOverrides            []string
)

func AddBootstrapConfigurationFlags(set *flag.FlagSet) {
	set.StringVar(&cliBootstrapClusterConfiguration.OperatorNamespace, "bootstrap-operator-namespace", "default", "operator namespace")
	set.StringVar(&cliBootstrapClusterConfiguration.ServiceName, "bootstrap-service-name", "operator", "operator service name")
	set.StringSliceVar(&cliContexts, "bootstrap-context", []string{}, "operator contexts")
	set.StringSliceVar(&cliServiceAddressOverrides, "bootstrap-service-address", []string{}, "operator service address overrides")
	set.StringSliceVar(&cliAPIServerOverrides, "bootstrap-server-address", []string{}, "operator kubernetes server address overrides")
	set.BoolVar(&cliBootstrapClusterConfiguration.BootstrapKubeconfigs, "bootstrap-kubeconfigs", false, "operator bootstrap kubeconfigs")
	set.BoolVar(&cliBootstrapClusterConfiguration.BootstrapTLS, "bootstrap-tls", false, "operator bootstrap tls")
}

func ConfigurationFromFlags() (BootstrapClusterConfiguration, error) {
	config := BootstrapClusterConfiguration{}
	type override struct {
		serviceAddress   string
		apiServerAddress string
	}
	contexts := map[string]override{}

	if len(cliContexts) < 3 {
		return config, errors.New("must specify at least 3 contexts")
	}

	for _, ctx := range cliContexts {
		contexts[ctx] = override{}
	}

	for _, addr := range cliAPIServerOverrides {
		parsed, err := url.Parse(addr)
		if err != nil {
			return config, errors.New("invalid override flag, must be of the form context-name+scheme://address")
		}
		tokens := strings.Split(parsed.Scheme, "+")
		if len(tokens) != 2 {
			return config, errors.New("invalid override flag, must be of the form context-name+scheme://address")
		}

		ctx, scheme := tokens[0], tokens[1]

		override, ok := contexts[ctx]
		if !ok {
			return config, fmt.Errorf("context %q not found", ctx)
		}

		override.apiServerAddress = scheme + "://" + parsed.Host
		contexts[ctx] = override
	}

	for _, addr := range cliServiceAddressOverrides {
		parsed, err := url.Parse(addr)
		if err != nil {
			return config, errors.New("invalid override flag, must be of the form context-name://address")
		}

		override, ok := contexts[parsed.Scheme]
		if !ok {
			return config, fmt.Errorf("context %q not found", parsed.Scheme)
		}

		override.serviceAddress = parsed.Host
		contexts[parsed.Scheme] = override
	}

	for _, ctx := range cliContexts {
		overrides := contexts[ctx]
		cliBootstrapClusterConfiguration.RemoteClusters = append(cliBootstrapClusterConfiguration.RemoteClusters, RemoteConfiguration{
			ContextName:    ctx,
			APIServer:      overrides.apiServerAddress,
			ServiceAddress: overrides.serviceAddress,
		})
	}

	return cliBootstrapClusterConfiguration, nil
}
