// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package console_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"emperror.dev/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/portforward"

	"github.com/redpanda-data/redpanda-operator/charts/console"
	"github.com/redpanda-data/redpanda-operator/pkg/gotohelm/helmette"
	"github.com/redpanda-data/redpanda-operator/pkg/helm"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
	"github.com/redpanda-data/redpanda/src/go/rpk/pkg/config"
)

type Client struct {
	Ctl           *kube.Ctl
	Release       *helm.Release
	consoleClient *portForwardClient
}

type portForwardClient struct {
	http.Client
	exposedPort int
	schema      string
}

func (c *Client) getStsPod(ctx context.Context, ordinal int) (*corev1.Pod, error) {
	return kube.Get[corev1.Pod](ctx, c.Ctl, kube.ObjectKey{
		Name:      fmt.Sprintf("%s-%d", c.Release.Name, ordinal),
		Namespace: c.Release.Namespace,
	})
}

func (c *Client) GetClusterHealth(ctx context.Context) (map[string]any, error) {
	pod, err := c.getStsPod(ctx, 0)
	if err != nil {
		return nil, err
	}

	client := c.consoleClient

	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s://127.0.0.1:%d/v1/cluster/health_overview", client.schema, client.exposedPort), nil)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	res, err := client.Do(req)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		return nil, errors.WithStack(err)
	}

	if res.StatusCode > 299 {
		return nil, errors.New("response above 299 HTTP code")
	}

	var clusterHealth map[string]any
	if err = json.Unmarshal(body, &clusterHealth); err != nil {
		return nil, errors.WithStack(err)
	}

	return clusterHealth, nil
}

// ExposeConsole will only Console service
func (c *Client) ExposeConsole(ctx context.Context, dot *helmette.Dot, out, errOut io.Writer) (func(), error) {
	pod, err := c.getStsPod(ctx, 0)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	availablePorts, cleanup, err := c.Ctl.PortForward(ctx, pod, out, errOut)
	if err != nil {
		return cleanup, errors.WithStack(err)
	}

	values := helmette.Unwrap[console.Values](dot.Values)

	defaultSecretName := fmt.Sprintf("%s-%s-%s", c.Release.Name, "default", "cert")

	secretName := defaultSecretName
	if len(values.Ingress.TLS) >  {
		secretName = values.Ingress.TLS[0].SecretName
	}
	
	adminClient, err := c.createClient(ctx,
		getInternalPort(rpYaml.Redpanda.AdminAPI, availablePorts),
		isTLSEnabled(rpYaml.Redpanda.AdminAPITLS),
		isMutualTLSEnabled(rpYaml.Redpanda.AdminAPITLS),
		secretName)
	if err != nil {
		return cleanup, errors.WithStack(err)
	}

	c.adminClients[pod.Name] = adminClient

	secretName = defaultSecretName
	cert = values.TLS.Certs[values.Listeners.SchemaRegistry.TLS.Cert]
	if ref := cert.ClientSecretRef; ref != nil {
		secretName = ref.Name
	}

	schemaClient, err := c.createClient(ctx,
		getInternalPort(rpYaml.SchemaRegistry.SchemaRegistryAPI, availablePorts),
		isTLSEnabled(rpYaml.SchemaRegistry.SchemaRegistryAPITLS),
		isMutualTLSEnabled(rpYaml.SchemaRegistry.SchemaRegistryAPITLS),
		secretName)
	if err != nil {
		return cleanup, errors.WithStack(err)
	}

	c.schemaClients[pod.Name] = schemaClient

	secretName = defaultSecretName
	cert = values.TLS.Certs[values.Listeners.HTTP.TLS.Cert]
	if ref := cert.ClientSecretRef; ref != nil {
		secretName = ref.Name
	}

	proxyClient, err := c.createClient(ctx,
		getInternalPort(rpYaml.Pandaproxy.PandaproxyAPI, availablePorts),
		isTLSEnabled(rpYaml.Pandaproxy.PandaproxyAPITLS),
		isMutualTLSEnabled(rpYaml.Pandaproxy.PandaproxyAPITLS),
		secretName)
	if err != nil {
		return cleanup, errors.WithStack(err)
	}

	c.proxyClients[pod.Name] = proxyClient

	return cleanup, err
}

func getInternalPort(addresses any, availablePorts []portforward.ForwardedPort) int {
	var adminListenerPort int
	switch v := addresses.(type) {
	case []config.NamedSocketAddress:
		for _, a := range v {
			if a.Name != "internal" {
				continue
			}
			adminListenerPort = a.Port
		}
	case []config.NamedAuthNSocketAddress:
		for _, a := range v {
			if a.Name != "internal" {
				continue
			}
			adminListenerPort = a.Port
		}
	}

	for _, p := range availablePorts {
		if int(p.Remote) == adminListenerPort {
			return int(p.Local)
		}
	}

	return 0
}
