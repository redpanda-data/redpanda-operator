// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package users

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/internal/testutils"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/redpanda"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/scram"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/kubectl/pkg/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestClient(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()

	testEnv := testutils.RedpandaTestEnv{}
	cfg, err := testEnv.StartRedpandaTestEnv(false)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	err = v1alpha2.AddToScheme(scheme.Scheme)
	require.NoError(t, err)

	c, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	require.NoError(t, err)
	require.NotNil(t, c)

	container, err := redpanda.Run(ctx, "docker.redpanda.com/redpandadata/redpanda:v23.2.8",
		redpanda.WithEnableKafkaAuthorization(),
		redpanda.WithEnableSASL(),
		redpanda.WithSuperusers("user"),
		redpanda.WithNewServiceAccount("user", "password"),
	)

	require.NoError(t, err)

	broker, err := container.KafkaSeedBroker(ctx)
	require.NoError(t, err)

	admin, err := container.AdminAPIAddress(ctx)
	require.NoError(t, err)

	kafkaClient, err := kgo.NewClient(kgo.SeedBrokers(broker), kgo.SASL(scram.Auth{
		User: "user",
		Pass: "password",
	}.AsSha256Mechanism()))
	require.NoError(t, err)

	rpadminClient, err := rpadmin.NewAdminAPI([]string{admin}, &rpadmin.BasicAuth{
		Username: "user",
		Password: "password",
	}, nil)
	require.NoError(t, err)

	usersClient, err := NewClient(ctx, c, kadm.NewClient(kafkaClient), rpadminClient)
	require.NoError(t, err)
	defer usersClient.Close()

	for _, mechanism := range []kadm.ScramMechanism{
		kadm.ScramSha256, kadm.ScramSha512,
	} {
		t.Run(mechanism.String(), func(t *testing.T) {
			username := "testuser" + strconv.Itoa(int(time.Now().UnixNano()))

			ok, err := usersClient.has(ctx, username)
			require.NoError(t, err)
			require.False(t, ok)

			err = usersClient.create(ctx, username, "password", mechanism)
			require.NoError(t, err)

			ok, err = usersClient.has(ctx, username)
			require.NoError(t, err)
			require.True(t, ok)

			err = usersClient.delete(ctx, username)
			require.NoError(t, err)

			ok, err = usersClient.has(ctx, username)
			require.NoError(t, err)
			require.False(t, ok)
		})
	}
}

func TestClientPasswordCreation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()

	testEnv := testutils.RedpandaTestEnv{}
	cfg, err := testEnv.StartRedpandaTestEnv(false)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	err = v1alpha2.AddToScheme(scheme.Scheme)
	require.NoError(t, err)

	c, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	require.NoError(t, err)
	require.NotNil(t, c)

	container, err := redpanda.Run(ctx, "docker.redpanda.com/redpandadata/redpanda:v23.2.8",
		redpanda.WithEnableKafkaAuthorization(),
		redpanda.WithEnableSASL(),
		redpanda.WithSuperusers("user"),
		redpanda.WithNewServiceAccount("user", "password"),
	)

	require.NoError(t, err)

	broker, err := container.KafkaSeedBroker(ctx)
	require.NoError(t, err)

	admin, err := container.AdminAPIAddress(ctx)
	require.NoError(t, err)

	kafkaClient, err := kgo.NewClient(kgo.SeedBrokers(broker), kgo.SASL(scram.Auth{
		User: "user",
		Pass: "password",
	}.AsSha256Mechanism()))
	require.NoError(t, err)

	rpadminClient, err := rpadmin.NewAdminAPI([]string{admin}, &rpadmin.BasicAuth{
		Username: "user",
		Password: "password",
	}, nil)
	require.NoError(t, err)

	usersClient, err := NewClient(ctx, c, kadm.NewClient(kafkaClient), rpadminClient)
	require.NoError(t, err)
	defer usersClient.Close()

	runTest := func(t *testing.T, username, password, secret string) {
		annotations := map[string]string{
			"test": "annotation",
		}
		labels := map[string]string{
			"test": "label",
		}

		user := &v1alpha2.User{
			ObjectMeta: metav1.ObjectMeta{
				Name:      username,
				Namespace: metav1.NamespaceDefault,
			},
			Spec: v1alpha2.UserSpec{
				ClusterSource: &v1alpha2.ClusterSource{
					ClusterRef: &v1alpha2.ClusterRef{
						Name: "bogus",
					},
				},
				Authentication: &v1alpha2.UserAuthenticationSpec{
					Type: ptr.To(v1alpha2.SASLMechanismScramSHA512),
					Password: v1alpha2.Password{
						Value: password,
						ValueFrom: &v1alpha2.PasswordSource{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: secret,
								},
								Key: "password",
							},
						},
					},
				},
				Template: &v1alpha2.UserTemplateSpec{
					Secret: &v1alpha2.ResourceTemplate{
						Metadata: v1alpha2.MetadataTemplate{
							Labels:      labels,
							Annotations: annotations,
						},
					},
				},
			},
		}

		require.NoError(t, c.Create(ctx, user))
		require.NoError(t, usersClient.Create(ctx, user))

		var secretObject corev1.Secret
		require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: metav1.NamespaceDefault, Name: secret}, &secretObject))
		require.Equal(t, labels, secretObject.Labels)
		require.Equal(t, annotations, secretObject.Annotations)
		require.NotEmpty(t, secretObject.Data["password"])

		if password != "" {
			require.Equal(t, password, string(secretObject.Data["password"]))
		}
	}

	for _, mechanism := range []kadm.ScramMechanism{
		kadm.ScramSha256, kadm.ScramSha512,
	} {
		t.Run("generated password "+mechanism.String(), func(t *testing.T) {
			username := "testuser" + strconv.Itoa(int(time.Now().UnixNano()))
			secret := "secret" + strconv.Itoa(int(time.Now().UnixNano()))
			runTest(t, username, "", secret)
		})

		t.Run("user specified password "+mechanism.String(), func(t *testing.T) {
			username := "testuser" + strconv.Itoa(int(time.Now().UnixNano()))
			password := "password" + strconv.Itoa(int(time.Now().UnixNano()))
			secret := "secret" + strconv.Itoa(int(time.Now().UnixNano()))
			runTest(t, username, password, secret)
		})
	}
}
