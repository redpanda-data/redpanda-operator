package kafka

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/go-logr/logr"
	krbclient "github.com/jcmturner/gokrb5/v8/client"
	krbconfig "github.com/jcmturner/gokrb5/v8/config"
	"github.com/jcmturner/gokrb5/v8/keytab"
	"github.com/redpanda-data/console/backend/pkg/config"
	"github.com/redpanda-data/helm-charts/pkg/kube"
	"github.com/redpanda-data/helm-charts/pkg/redpanda"
	redpandav1alpha1 "github.com/redpanda-data/redpanda-operator/src/go/k8s/api/cluster.redpanda.com/v1alpha1"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/src/go/k8s/api/redpanda/v1alpha2"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl"
	"github.com/twmb/franz-go/pkg/sasl/aws"
	"github.com/twmb/franz-go/pkg/sasl/kerberos"
	"github.com/twmb/franz-go/pkg/sasl/oauth"
	"github.com/twmb/franz-go/pkg/sasl/plain"
	"github.com/twmb/franz-go/pkg/sasl/scram"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var ErrEmptyBrokerList = errors.New("empty broker list")

type ClientFactory struct {
	client.Client
	logger          logr.Logger
	metricNamespace *string
	config          *rest.Config
	dialer          redpanda.DialContextFunc
}

func NewClientFactory(config *rest.Config) (*ClientFactory, error) {
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		return nil, err
	}

	if err := redpandav1alpha2.AddToScheme(s); err != nil {
		return nil, err
	}

	if err := redpandav1alpha1.AddToScheme(s); err != nil {
		return nil, err
	}

	client, err := client.New(config, client.Options{Scheme: s})
	if err != nil {
		return nil, err
	}

	return &ClientFactory{
		Client: client,
		config: config,
		logger: logr.Discard(),
	}, nil
}

func (c *ClientFactory) WithDialer(dialer redpanda.DialContextFunc) *ClientFactory {
	c.dialer = dialer
	return c
}

func (c *ClientFactory) WithMetricNamespace(metricNamespace string) *ClientFactory {
	c.metricNamespace = &metricNamespace
	return c
}

func (c *ClientFactory) WithLogger(logger logr.Logger) *ClientFactory {
	c.logger = logger
	return c
}

// Admin returns a client able to communicate with the cluster defined by the given KafkaAPISpec.
func (c *ClientFactory) Admin(ctx context.Context, namespace string, spec *redpandav1alpha1.KafkaAPISpec, opts ...kgo.Opt) (*kadm.Client, error) {
	client, err := c.GetClient(ctx, namespace, spec, opts...)
	if err != nil {
		return nil, err
	}

	return kadm.NewClient(client), nil
}

// ClusterAdmin returns a client able to communicate with the given Redpanda cluster.
func (c *ClientFactory) ClusterAdmin(ctx context.Context, cluster *redpandav1alpha2.Redpanda, opts ...kgo.Opt) (*kadm.Client, error) {
	release, partials, err := releaseAndPartialsFor(cluster)
	if err != nil {
		return nil, err
	}

	config := kube.RestToConfig(c.config)
	client, err := redpanda.KafkaClient(config, release, partials, c.dialer, opts...)
	if err != nil {
		return nil, err
	}

	return kadm.NewClient(client), nil
}

// GetClient returns a simple kgo.Client able to communicate with the given cluster specified via KafkaAPISpec.
func (c *ClientFactory) GetClient(ctx context.Context, namespace string, spec *redpandav1alpha1.KafkaAPISpec, opts ...kgo.Opt) (*kgo.Client, error) {
	kopts, err := c.configFromSpec(ctx, namespace, spec)
	if err != nil {
		return nil, err
	}
	return kgo.NewClient(append(opts, kopts...)...)
}

func (c *ClientFactory) configFromSpec(ctx context.Context, namespace string, spec *redpandav1alpha1.KafkaAPISpec) ([]kgo.Opt, error) {
	if len(spec.Brokers) == 0 {
		return nil, ErrEmptyBrokerList
	}
	opts := []kgo.Opt{
		kgo.SeedBrokers(spec.Brokers...),
	}

	metricsLabel := "redpanda_operator"
	if c.metricNamespace != nil && *c.metricNamespace != "" {
		metricsLabel = *c.metricNamespace
	}

	hooks := newClientHooks(c.logger, metricsLabel)

	// Create Logger
	opts = append(opts, kgo.WithLogger(wrapLogger(c.logger)), kgo.WithHooks(hooks))

	if spec.SASL != nil {
		sasl, err := c.configureSpecSASL(ctx, namespace, spec)
		if err != nil {
			return nil, err
		}

		opts = append(opts, sasl)
	}

	if spec.TLS != nil {
		tlsConfig, err := c.configureSpecTLS(ctx, namespace, spec)
		if err != nil {
			return nil, err
		}

		if c.dialer != nil {
			opts = append(opts, kgo.Dialer(wrapTLSDialer(c.dialer, tlsConfig)))
		} else {
			dialer := &tls.Dialer{
				NetDialer: &net.Dialer{Timeout: 10 * time.Second},
				Config:    tlsConfig,
			}
			opts = append(opts, kgo.Dialer(dialer.DialContext))
		}
	} else if c.dialer != nil {
		opts = append(opts, kgo.Dialer(c.dialer))
	}

	return opts, nil
}

func (c *ClientFactory) configureSpecTLS(ctx context.Context, namespace string, spec *redpandav1alpha1.KafkaAPISpec) (*tls.Config, error) {
	var caCertPool *x509.CertPool

	// Root CA
	if spec.TLS.CaCert != nil {
		ca, err := spec.TLS.CaCert.GetValue(ctx, c.Client, namespace, "ca.crt")
		if err != nil {
			return nil, fmt.Errorf("failed to read ca certificate secret: %w", err)
		}

		caCertPool = x509.NewCertPool()
		isSuccessful := caCertPool.AppendCertsFromPEM(ca)
		if !isSuccessful {
			c.logger.Info("failed to append ca file to cert pool, is this a valid PEM format?")
		}
	}

	// If configured load TLS cert & key - Mutual TLS
	var certificates []tls.Certificate
	if spec.TLS.Cert != nil && spec.TLS.Key != nil {
		// 1. Read certificates
		cert, err := spec.TLS.Cert.GetValue(ctx, c.Client, namespace, "tls.crt")
		if err != nil {
			return nil, fmt.Errorf("failed to read certificate secret: %w", err)
		}

		certData := cert

		key, err := spec.TLS.Cert.GetValue(ctx, c.Client, namespace, "tls.key")
		if err != nil {
			return nil, fmt.Errorf("failed to read key certificate secret: %w", err)
		}

		keyData := key

		// 2. Check if private key needs to be decrypted. Decrypt it if passphrase is given, otherwise return error
		pemBlock, _ := pem.Decode(keyData)
		if pemBlock == nil {
			return nil, fmt.Errorf("no valid private key found") // nolint:goerr113 // this error will not be handled by operator
		}

		tlsCert, err := tls.X509KeyPair(certData, keyData)
		if err != nil {
			return nil, fmt.Errorf("cannot parse pem: %w", err)
		}
		certificates = []tls.Certificate{tlsCert}
	}

	return &tls.Config{
		//nolint:gosec // InsecureSkipVerify may be true upon user's responsibility.
		InsecureSkipVerify: spec.TLS.InsecureSkipTLSVerify,
		Certificates:       certificates,
		RootCAs:            caCertPool,
	}, nil
}

func (c *ClientFactory) configureSpecSASL(ctx context.Context, namespace string, spec *redpandav1alpha1.KafkaAPISpec) (kgo.Opt, error) {
	switch spec.SASL.Mechanism {
	// SASL Plain
	case config.SASLMechanismPlain:
		p, err := spec.SASL.Password.GetValue(ctx, c.Client, namespace, "password")
		if err != nil {
			return nil, fmt.Errorf("unable to fetch sasl plain password: %w", err)
		}

		return kgo.SASL(plain.Auth{
			User: spec.SASL.Username,
			Pass: string(p),
		}.AsMechanism()), nil

	// SASL SCRAM
	case config.SASLMechanismScramSHA256, config.SASLMechanismScramSHA512:
		p, err := spec.SASL.Password.GetValue(ctx, c.Client, namespace, "password")
		if err != nil {
			return nil, fmt.Errorf("unable to fetch sasl scram password: %w", err)
		}

		var mechanism sasl.Mechanism
		scramAuth := scram.Auth{
			User: spec.SASL.Username,
			Pass: string(p),
		}

		if spec.SASL.Mechanism == config.SASLMechanismScramSHA256 {
			c.logger.V(traceLevel).Info("configuring SCRAM-SHA-256 mechanism")
			mechanism = scramAuth.AsSha256Mechanism()
		}

		if spec.SASL.Mechanism == config.SASLMechanismScramSHA512 {
			c.logger.V(traceLevel).Info("configuring SCRAM-SHA-512 mechanism")
			mechanism = scramAuth.AsSha512Mechanism()
		}

		return kgo.SASL(mechanism), nil

	// OAuth Bearer
	case config.SASLMechanismOAuthBearer:
		t, err := spec.SASL.OAUth.Token.GetValue(ctx, c.Client, namespace, "token")
		if err != nil {
			return nil, fmt.Errorf("unable to fetch token: %w", err)
		}

		return kgo.SASL(oauth.Auth{
			Token: string(t),
		}.AsMechanism()), nil

	// Kerberos
	case config.SASLMechanismGSSAPI:
		c.logger.V(traceLevel).Info("configuring SCRAM-SHA-512 mechanism")
		var krbClient *krbclient.Client

		kerbCfg, err := krbconfig.Load(spec.SASL.GSSAPIConfig.KerberosConfigPath)
		if err != nil {
			return nil, fmt.Errorf("creating kerberos config from specified config (%s) filepath: %w", spec.SASL.GSSAPIConfig.KerberosConfigPath, err)
		}

		switch spec.SASL.GSSAPIConfig.AuthType {
		case "USER_AUTH":
			p, err := spec.SASL.GSSAPIConfig.Password.GetValue(ctx, c.Client, namespace, "password")
			if err != nil {
				return nil, fmt.Errorf("unable to fetch sasl gssapi password: %w", err)
			}

			krbClient = krbclient.NewWithPassword(
				spec.SASL.GSSAPIConfig.Username,
				spec.SASL.GSSAPIConfig.Realm,
				string(p),
				kerbCfg,
				krbclient.DisablePAFXFAST(!spec.SASL.GSSAPIConfig.EnableFast),
			)

		case "KEYTAB_AUTH":
			ktb, err := keytab.Load(spec.SASL.GSSAPIConfig.KeyTabPath)
			if err != nil {
				return nil, fmt.Errorf("loading keytab from (%s) key tab path: %w", spec.SASL.GSSAPIConfig.KeyTabPath, err)
			}

			krbClient = krbclient.NewWithKeytab(
				spec.SASL.GSSAPIConfig.Username,
				spec.SASL.GSSAPIConfig.Realm,
				ktb,
				kerbCfg,
				krbclient.DisablePAFXFAST(!spec.SASL.GSSAPIConfig.EnableFast),
			)
		}

		return kgo.SASL(kerberos.Auth{
			Client:           krbClient,
			Service:          spec.SASL.GSSAPIConfig.ServiceName,
			PersistAfterAuth: true,
		}.AsMechanism()), nil

	// AWS MSK IAM
	case config.SASLMechanismAWSManagedStreamingIAM:
		s, err := spec.SASL.AWSMskIam.SecretKey.GetValue(ctx, c.Client, namespace, "secret")
		if err != nil {
			return nil, fmt.Errorf("unable to fetch aws msk secret key: %w", err)
		}

		t, err := spec.SASL.AWSMskIam.SessionToken.GetValue(ctx, c.Client, namespace, "token")
		if err != nil {
			return nil, fmt.Errorf("unable to fetch aws msk secret key: %w", err)
		}

		return kgo.SASL(aws.Auth{
			AccessKey:    spec.SASL.AWSMskIam.AccessKey,
			SecretKey:    string(s),
			SessionToken: string(t),
			UserAgent:    spec.SASL.AWSMskIam.UserAgent,
		}.AsManagedStreamingIAMMechanism()), nil
	}

	return nil, fmt.Errorf("unsupported sasl mechanism: %s", spec.SASL.Mechanism)
}
