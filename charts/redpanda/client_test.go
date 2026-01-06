// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package redpanda_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/sr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/portforward"
	"sigs.k8s.io/yaml"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/client"
	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
	"github.com/redpanda-data/redpanda-operator/pkg/helm"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
)

type Client struct {
	Ctl          *kube.Ctl
	state        *redpanda.RenderState
	proxyClients map[string]*portForwardClient
}

func newClient(t *testing.T, ctl *kube.Ctl, release *helm.Release, values any) *Client {
	dot, err := redpanda.Chart.Dot(
		ctl.RestConfig(),
		helmette.Release{Name: release.Name, Namespace: release.Namespace},
		values,
	)
	require.NoError(t, err)

	state, err := redpanda.RenderStateFromDot(dot)
	require.NoError(t, err)

	return &Client{Ctl: ctl, state: state}
}

type portForwardClient struct {
	http.Client
	exposedPort int
	schema      string
}

func (c *Client) getStsPod(ctx context.Context, ordinal int) (*corev1.Pod, error) {
	return kube.Get[corev1.Pod](ctx, c.Ctl, kube.ObjectKey{
		Name:      fmt.Sprintf("%s-%d", c.state.Release.Name, ordinal),
		Namespace: c.state.Release.Namespace,
	})
}

func (c *Client) CreateTopic(ctx context.Context, topicName string) (map[string]any, error) {
	pod, err := c.getStsPod(ctx, 0)
	if err != nil {
		return nil, err
	}

	var stderr bytes.Buffer
	var stdout bytes.Buffer
	if err = c.Ctl.Exec(ctx, pod, kube.ExecOptions{
		Command: []string{"bash", "-c", fmt.Sprintf(`rpk topic create %s -r 1 -p 3`, topicName)},
		Stdout:  &stdout,
		Stderr:  &stderr,
	}); err != nil {
		return nil, errors.Wrapf(err, "STDOUT:\n%s\n\nSTDERR:\n%s\n", stdout.String(), stderr.String())
	}

	var cfg map[string]any
	if err = yaml.Unmarshal(stderr.Bytes(), &cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Client) KafkaProduce(ctx context.Context, input, topicName string) (string, error) {
	pod, err := c.getStsPod(ctx, 0)
	if err != nil {
		return "", err
	}

	var stderr bytes.Buffer
	var stdout bytes.Buffer
	if err = c.Ctl.Exec(ctx, pod, kube.ExecOptions{
		Command: []string{"bash", "-c", fmt.Sprintf(`echo %s | rpk topic produce %s`, input, topicName)},
		Stdout:  &stdout,
		Stderr:  &stderr,
	}); err != nil || stderr.Len() > 0 {
		return "", errors.Wrapf(err, "STDOUT:\n%s\n\nSTDERR:\n%s\n", stdout.String(), stderr.String())
	}

	return stdout.String(), nil
}

func (c *Client) KafkaConsume(ctx context.Context, topicName string) (map[string]any, error) {
	pod, err := c.getStsPod(ctx, 0)
	if err != nil {
		return nil, err
	}

	var stderr bytes.Buffer
	var stdout bytes.Buffer
	if err = c.Ctl.Exec(ctx, pod, kube.ExecOptions{
		Command: []string{"bash", "-c", fmt.Sprintf(`rpk topic consume %s -n 1`, topicName)},
		Stdout:  &stdout,
		Stderr:  &stderr,
	}); err != nil || stderr.Len() > 0 {
		return nil, errors.Wrapf(err, "STDOUT:\n%s\n\nSTDERR:\n%s\n", stdout.String(), stderr.String())
	}

	var event map[string]any
	if err = json.Unmarshal(stdout.Bytes(), &event); err != nil {
		return nil, err
	}

	return event, nil
}

func (c *Client) GetClusterHealth(ctx context.Context) (rpadmin.ClusterHealthOverview, error) {
	dialer := kube.NewPodDialer(c.Ctl.RestConfig())

	adminClient, err := client.AdminClient(c.state, dialer.DialContext)
	if err != nil {
		return rpadmin.ClusterHealthOverview{}, err
	}

	defer adminClient.Close()

	return adminClient.GetHealthOverview(ctx)
}

func (c *Client) GetSuperusers(ctx context.Context) ([]string, error) {
	dialer := kube.NewPodDialer(c.Ctl.RestConfig())

	adminClient, err := client.AdminClient(c.state, dialer.DialContext)
	if err != nil {
		return nil, err
	}

	defer adminClient.Close()

	config, err := adminClient.Config(ctx, false)
	if err != nil {
		return nil, err
	}

	sus := config["superusers"].([]any)
	superusers := make([]string, len(sus))
	for i, su := range sus {
		superusers[i] = su.(string)
	}

	return superusers, nil
}

func (c *Client) QuerySupportedFormats(ctx context.Context) ([]string, error) {
	dialer := kube.NewPodDialer(c.Ctl.RestConfig())

	srClient, err := client.SchemaRegistryClient(c.state, dialer.DialContext)
	if err != nil {
		return nil, err
	}

	types, err := srClient.SupportedTypes(ctx)
	if err != nil {
		return nil, err
	}

	formats := make([]string, len(types))
	for i, t := range types {
		formats[i] = t.String()
	}

	return formats, nil
}

func (c *Client) RegisterSchema(ctx context.Context, schema map[string]any) (sr.SubjectSchema, error) {
	dialer := kube.NewPodDialer(c.Ctl.RestConfig())

	srClient, err := client.SchemaRegistryClient(c.state, dialer.DialContext)
	if err != nil {
		return sr.SubjectSchema{}, err
	}

	schemaBytes, err := json.Marshal(schema)
	if err != nil {
		return sr.SubjectSchema{}, nil
	}

	subject, err := srClient.CreateSchema(ctx, "sensor-value", sr.Schema{
		Schema: string(schemaBytes),
	})
	if err != nil {
		return sr.SubjectSchema{}, err
	}

	return subject, nil
}

func (c *Client) RetrieveSchema(ctx context.Context, id int) (sr.Schema, error) {
	dialer := kube.NewPodDialer(c.Ctl.RestConfig())

	srClient, err := client.SchemaRegistryClient(c.state, dialer.DialContext)
	if err != nil {
		return sr.Schema{}, err
	}

	schema, err := srClient.SchemaByID(ctx, id)
	if err != nil {
		return sr.Schema{}, err
	}

	return schema, nil
}

func (c *Client) ListRegistrySubjects(ctx context.Context) ([]string, error) {
	dialer := kube.NewPodDialer(c.Ctl.RestConfig())

	srClient, err := client.SchemaRegistryClient(c.state, dialer.DialContext)
	if err != nil {
		return nil, err
	}

	return srClient.Subjects(ctx)
}

func (c *Client) SoftDeleteSchema(ctx context.Context, subject string, version int) error {
	dialer := kube.NewPodDialer(c.Ctl.RestConfig())

	srClient, err := client.SchemaRegistryClient(c.state, dialer.DialContext)
	if err != nil {
		return err
	}

	return srClient.DeleteSchema(ctx, subject, version, sr.SoftDelete)
}

func (c *Client) HardDeleteSchema(ctx context.Context, subject string, version int) error {
	dialer := kube.NewPodDialer(c.Ctl.RestConfig())

	srClient, err := client.SchemaRegistryClient(c.state, dialer.DialContext)
	if err != nil {
		return err
	}

	return srClient.DeleteSchema(ctx, subject, version, sr.HardDelete)
}

func (c *Client) ListTopics(ctx context.Context) ([]string, error) {
	pod, err := c.getStsPod(ctx, 0)
	if err != nil {
		return nil, err
	}

	client := c.proxyClients[pod.Name]

	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s://127.0.0.1:%d/topics", client.schema, client.exposedPort), nil)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	req.Header.Add("Content-Type", "application/vnd.kafka.json.v2+json")

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

	var resp []string
	if err = json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}

	return resp, nil
}

func (c *Client) SendEventToTopic(ctx context.Context, records map[string]any, topicName string) (string, error) {
	pod, err := c.getStsPod(ctx, 0)
	if err != nil {
		return "", err
	}

	recordsStr, err := json.Marshal(records)
	if err != nil {
		return "", err
	}

	client := c.proxyClients[pod.Name]

	req, err := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s://127.0.0.1:%d/topics/%s", client.schema, client.exposedPort, topicName), bytes.NewReader(recordsStr))
	if err != nil {
		return "", errors.WithStack(err)
	}
	req.Header.Add("Content-Type", "application/vnd.kafka.json.v2+json")

	res, err := client.Do(req)
	if err != nil {
		return "", errors.WithStack(err)
	}

	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		return "", errors.WithStack(err)
	}

	if res.StatusCode > 299 {
		return "", errors.New("response above 299 HTTP code")
	}

	return string(body), nil
}

func (c *Client) RetrieveEventFromTopic(ctx context.Context, topicName string, partitionNumber int) (string, error) {
	pod, err := c.getStsPod(ctx, 0)
	if err != nil {
		return "", err
	}

	client := c.proxyClients[pod.Name]

	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s://127.0.0.1:%d/topics/%s/partitions/%d/records?offset=0&timeout=1000&max_bytes=100000", client.schema, client.exposedPort, topicName, partitionNumber), nil)
	if err != nil {
		return "", errors.WithStack(err)
	}
	req.Header.Add("Accept", "application/vnd.kafka.json.v2+json")

	res, err := client.Do(req)
	if err != nil {
		return "", errors.WithStack(err)
	}

	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		return "", errors.WithStack(err)
	}

	if res.StatusCode > 299 {
		return "", errors.Newf("response above 299 HTTP code (Status Code: %d) (Body: %s)", res.StatusCode, body)
	}

	return string(body), nil
}

// ExposeRedpandaCluster will only expose ports from first (`pod-0`) kafka, Admin API,
// schema registry and HTTP proxy (aka panda proxy) ports.
//
// As future improvement function could expose all ports for each Redpanda. As possible
// returned map of Pod name to map of listener and port could be provided.
func (c *Client) ExposeRedpandaCluster(ctx context.Context, out, errOut io.Writer) (func(), error) {
	pod, err := c.getStsPod(ctx, 0)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	ports, cleanup, err := c.Ctl.PortForward(ctx, pod, out, errOut)
	if err != nil {
		return cleanup, errors.WithStack(err)
	}

	if c.proxyClients == nil {
		c.proxyClients = make(map[string]*portForwardClient)
	}

	proxyClient, err := c.createClient(ctx, ports, c.state.Values.Listeners.HTTP.AsString())
	if err != nil {
		return cleanup, errors.WithStack(err)
	}

	c.proxyClients[pod.Name] = proxyClient

	return cleanup, err
}

func (c *Client) createClient(ctx context.Context, ports []portforward.ForwardedPort, cfg redpanda.ListenerConfig[string]) (*portForwardClient, error) {
	schema := "http"
	var rootCAs *x509.CertPool
	var certs []tls.Certificate
	if cfg.TLS.IsEnabled(&c.state.Values.TLS) {
		cert := c.state.Values.TLS.Certs.MustGet(cfg.TLS.Cert)

		schema = "https"
		s, err := kube.Get[corev1.Secret](ctx, c.Ctl, kube.ObjectKey{
			Name:      cert.ServerSecretName(c.state, cfg.TLS.Cert),
			Namespace: c.state.Release.Namespace,
		})
		if err != nil {
			return nil, errors.WithStack(err)
		}

		rootCAs = x509.NewCertPool()
		ok := rootCAs.AppendCertsFromPEM(s.Data["ca.crt"])
		if !ok {
			return nil, errors.WithStack(errors.New("failed to parse CA certificate"))
		}

		if cfg.TLS.RequireClientAuth {
			cert, err := tls.X509KeyPair(s.Data["tls.crt"], s.Data["tls.key"])
			if err != nil {
				return nil, errors.WithStack(err)
			}
			certs = append(certs, cert)
		}
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			Certificates: certs,
			RootCAs:      rootCAs,
			// Available subject alternative names are defined in certs.go
			ServerName: fmt.Sprintf("%s.%s", c.state.Release.Name, c.state.Release.Namespace),
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

	for _, port := range ports {
		if port.Remote == uint16(cfg.Port) {
			return &portForwardClient{
				httpClient,
				int(port.Local),
				schema,
			}, nil
		}
	}

	return nil, errors.Newf("remote port not forwarded: %d", cfg.Port)
}
