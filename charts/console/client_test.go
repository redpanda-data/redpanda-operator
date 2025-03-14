// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package console

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redpanda-data/redpanda-operator/pkg/gotohelm/helmette"
	"github.com/redpanda-data/redpanda-operator/pkg/helm"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
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

func (c *Client) getConsolePod(ctx context.Context) (*corev1.Pod, error) {
	deploys, err := kube.List[appsv1.DeploymentList](ctx, c.Ctl,
		k8sclient.InNamespace(c.Release.Namespace),
	)
	if err != nil {
		return nil, err
	}

	for _, deploy := range deploys.Items {
		fmt.Println(deploy.Name)
	}

	deployment, err := kube.Get[appsv1.Deployment](ctx, c.Ctl, kube.ObjectKey{
		Name:      c.Release.Name + "-console",
		Namespace: c.Release.Namespace,
	})
	if err != nil {
		return nil, err
	}

	pods, err := kube.List[corev1.PodList](ctx, c.Ctl,
		k8sclient.InNamespace(deployment.Namespace),
		k8sclient.MatchingLabels(deployment.Spec.Selector.MatchLabels))
	if err != nil {
		return nil, err
	}

	if len(pods.Items) == 0 {
		return nil, errors.New("no pods found")
	}

	return &pods.Items[0], nil
}

// ExposeConsole will only Console service
func (c *Client) ExposeConsole(ctx context.Context, dot *helmette.Dot, out, errOut io.Writer) (func(), error) {
	pod, err := c.getConsolePod(ctx)
	if err != nil {
		return nil, err
	}

	availablePorts, cleanup, err := c.Ctl.PortForward(ctx, pod, out, errOut)
	if err != nil {
		return cleanup, err
	}

	values := helmette.Unwrap[Values](dot.Values)

	defaultSecretName := fmt.Sprintf("%s-%s-%s", c.Release.Name, "default", "cert")

	secretName := defaultSecretName
	if len(values.Ingress.TLS) > 0 {
		secretName = values.Ingress.TLS[0].SecretName
	}

	httpConsoleClient, err := c.createClient(ctx,
		int(availablePorts[0].Local),
		false,
		secretName)
	if err != nil {
		return cleanup, err
	}

	c.consoleClient = httpConsoleClient

	return cleanup, err
}

func (c *Client) GetDebugVars(ctx context.Context) (string, error) {
	client := c.consoleClient

	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s://127.0.0.1:%d/debug/vars", client.schema, client.exposedPort), nil)
	if err != nil {
		return "", err
	}

	res, err := client.Do(req)
	if err != nil {
		return "", err
	}

	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		return "", err
	}

	if res.StatusCode > 299 {
		return "", errors.New("response above 299 HTTP code")
	}

	return string(body), nil
}

func (c *Client) createClient(ctx context.Context, port int, tlsEnabled bool, tlsK8SSecretName string) (*portForwardClient, error) {
	if port == 0 {
		return nil, errors.New("admin internal listener port not found")
	}

	schema := "http"
	var rootCAs *x509.CertPool
	var certs []tls.Certificate
	if tlsEnabled {
		schema = "https"
		s, err := kube.Get[corev1.Secret](ctx, c.Ctl, kube.ObjectKey{
			Name:      tlsK8SSecretName,
			Namespace: c.Release.Namespace,
		})
		if err != nil {
			return nil, err
		}

		rootCAs = x509.NewCertPool()
		ok := rootCAs.AppendCertsFromPEM(s.Data["ca.crt"])
		if !ok {
			return nil, errors.New("failed to parse CA certificate")
		}
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			Certificates: certs,
			RootCAs:      rootCAs,
			// Available subject alternative names are defined in certs.go
			ServerName: fmt.Sprintf("%s.%s", c.Release.Name, c.Release.Namespace),
		},
		TLSHandshakeTimeout:   10 * time.Second,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
	}

	httpClient := http.Client{
		Transport: transport,
	}

	pfc := &portForwardClient{
		httpClient,
		port,
		schema,
	}

	return pfc, nil
}
