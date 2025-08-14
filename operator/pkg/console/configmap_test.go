package console

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
)

func TestGenerateConsoleConfig_EmptyScopes(t *testing.T) {
	client := fake.NewClientBuilder().Build()

	// Create console object with empty scopes in SecretStore
	console := &vectorizedv1alpha1.Console{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-console",
			Namespace: "test-namespace",
		},
		Spec: vectorizedv1alpha1.ConsoleSpec{
			Server: vectorizedv1alpha1.Server{
				ServerGracefulShutdownTimeout: &metav1.Duration{Duration: 5 * time.Second},
				HTTPServerReadTimeout:         &metav1.Duration{Duration: 5 * time.Second},
				HTTPServerWriteTimeout:        &metav1.Duration{Duration: 5 * time.Second},
				HTTPServerIdleTimeout:         &metav1.Duration{Duration: 5 * time.Second},
			},
			Connect: vectorizedv1alpha1.Connect{
				ConnectTimeout: &metav1.Duration{Duration: 5 * time.Second},
				ReadTimeout:    &metav1.Duration{Duration: 5 * time.Second},
				RequestTimeout: &metav1.Duration{Duration: 5 * time.Second},
			},
			SecretStore: &vectorizedv1alpha1.SecretStore{
				Enabled:          true,
				SecretNamePrefix: "test-prefix",
				Scopes:           []string{}, // Empty scopes array
			},
		},
	}

	// Create cluster object
	cluster := &vectorizedv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "test-namespace",
		},
		Spec: vectorizedv1alpha1.ClusterSpec{
			Configuration: vectorizedv1alpha1.RedpandaConfig{
				KafkaAPI: []vectorizedv1alpha1.KafkaAPI{
					{
						Port: 9092,
						Name: "test-kafka",
					},
				},
			},
		},
	}

	// Setup Kafka secret that the function will try to read
	kafkaSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-console-kafka-sa-secret",
			Namespace: "test-namespace",
		},
		Data: map[string][]byte{
			corev1.BasicAuthUsernameKey: []byte("test-user"),
		},
	}

	// Add secret to fake client
	err := client.Create(context.Background(), kafkaSecret)
	require.NoError(t, err)

	// Use NewConfigMap from console package
	cm := NewConfigMap(client, controller.UnifiedScheme, console, cluster, logr.Discard())
	// Generate the console config
	configYaml, err := cm.generateConsoleConfig(context.Background(), "test-user")
	require.NoError(t, err)
	require.NotEmpty(t, configYaml)
	require.False(t, strings.Contains(configYaml, "scopes"), "Config YAML should not contain scopes configuration when empty")
}
