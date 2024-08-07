package clusterredpandacom

import (
	"context"
	"errors"

	"github.com/go-logr/logr"
	"github.com/twmb/franz-go/pkg/kgo"
	ctrl "sigs.k8s.io/controller-runtime"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redpanda-data/redpanda-operator/src/go/k8s/api/cluster.redpanda.com/v1alpha1"
)

var ErrEmptyBrokerList = errors.New("empty broker list")

// Reference implementation https://github.com/redpanda-data/console/blob/0ba44b236b6ddd7191da015f44a9302fc13665ec/backend/pkg/kafka/config_helper.go#L44

func newKgoConfig(ctx context.Context, cl k8sclient.Client, topic *v1alpha1.Topic, log logr.Logger) ([]kgo.Opt, error) { // nolint:funlen // This is copy of the console
	var err error

	log.WithName("newKgoConfig")

	if len(topic.Spec.KafkaAPISpec.Brokers) == 0 {
		return nil, ErrEmptyBrokerList
	}
	opts := []kgo.Opt{
		kgo.SeedBrokers(topic.Spec.KafkaAPISpec.Brokers...),
	}

	metricsLabel := "redpanda_operator"
	if topic.Spec.MetricsNamespace != nil && *topic.Spec.MetricsNamespace != "" {
		metricsLabel = *topic.Spec.MetricsNamespace
	}

	hooks := newClientHooks(ctrl.Log, metricsLabel)

	// Create Logger
	kgoLogger := KgoZapLogger{
		logger: ctrl.Log,
	}
	opts = append(opts, kgo.WithLogger(kgoLogger), kgo.WithHooks(hooks))

	if topic.Spec.KafkaAPISpec.SASL != nil {
		opts, err = topic.Spec.KafkaAPISpec.ConfigureSASL(ctx, topic.Namespace, cl, opts, log)
		if err != nil {
			return nil, err
		}
	}

	if topic.Spec.KafkaAPISpec.TLS != nil {
		opts, err = topic.Spec.KafkaAPISpec.ConfigureTLS(ctx, topic.Namespace, cl, opts, log)
		if err != nil {
			return nil, err
		}
	}

	return opts, nil
}
