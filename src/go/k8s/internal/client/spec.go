package client

import (
	"context"
	"crypto/tls"
	"net"
	"time"

	"github.com/redpanda-data/common-go/rpadmin"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/src/go/k8s/api/redpanda/v1alpha2"
	"github.com/twmb/franz-go/pkg/kgo"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// KafkaForSpec returns a simple kgo.Client able to communicate with the given cluster specified via KafkaAPISpec.
func (c *Factory) kafkaForSpec(ctx context.Context, namespace string, metricNamespace *string, spec *redpandav1alpha2.KafkaAPISpec, opts ...kgo.Opt) (*kgo.Client, error) {
	logger := log.FromContext(ctx)

	if len(spec.Brokers) == 0 {
		return nil, ErrEmptyBrokerList
	}
	kopts := []kgo.Opt{
		kgo.SeedBrokers(spec.Brokers...),
	}

	metricsLabel := "redpanda_operator"
	if metricNamespace != nil && *metricNamespace != "" {
		metricsLabel = *metricNamespace
	}

	hooks := newClientHooks(logger, metricsLabel)

	// Create Logger
	kopts = append(kopts, kgo.WithLogger(wrapLogger(logger)), kgo.WithHooks(hooks))

	if spec.SASL != nil {
		sasl, err := c.configureKafkaSpecSASL(ctx, namespace, spec)
		if err != nil {
			return nil, err
		}

		kopts = append(kopts, sasl)
	}

	if spec.TLS != nil {
		tlsConfig, err := c.configureSpecTLS(ctx, namespace, spec.TLS)
		if err != nil {
			return nil, err
		}

		if c.dialer != nil {
			kopts = append(kopts, kgo.Dialer(wrapTLSDialer(c.dialer, tlsConfig)))
		} else {
			dialer := &tls.Dialer{
				NetDialer: &net.Dialer{Timeout: 10 * time.Second},
				Config:    tlsConfig,
			}
			kopts = append(kopts, kgo.Dialer(dialer.DialContext))
		}
	} else if c.dialer != nil {
		kopts = append(kopts, kgo.Dialer(c.dialer))
	}

	return kgo.NewClient(append(opts, kopts...)...)
}

func (c *Factory) redpandaAdminForSpec(ctx context.Context, namespace string, spec *redpandav1alpha2.AdminAPISpec) (*rpadmin.AdminAPI, error) {
	if len(spec.URLs) == 0 {
		return nil, ErrEmptyURLList
	}

	var err error
	var tlsConfig *tls.Config
	if spec.TLS != nil {
		tlsConfig, err = c.configureSpecTLS(ctx, namespace, spec.TLS)
		if err != nil {
			return nil, err
		}
	}

	var auth rpadmin.Auth
	var username, password, token string
	username, password, token, err = c.configureAdminSpecSASL(ctx, namespace, spec)
	if err != nil {
		return nil, err
	}

	switch {
	case username != "":
		auth = &rpadmin.BasicAuth{
			Username: username,
			Password: password,
		}
	case token != "":
		auth = &rpadmin.BearerToken{
			Token: token,
		}
	default:
		auth = &rpadmin.NopAuth{}
	}

	return rpadmin.NewAdminAPIWithDialer(spec.URLs, auth, tlsConfig, c.dialer)
}
