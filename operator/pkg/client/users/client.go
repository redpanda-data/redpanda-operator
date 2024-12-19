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
	"slices"

	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kmsg"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// Client is a high-level client for managing users in a Redpanda cluster.
type Client struct {
	kafkaAdminClient  *kadm.Client
	adminClient       *rpadmin.AdminAPI
	client            client.Client
	generator         *passwordGenerator
	scramAPISupported bool
}

// NewClient returns a high-level client that is able to manage users in a Redpanda cluster.
func NewClient(ctx context.Context, kubeClient client.Client, kafkaAdminClient *kadm.Client, adminClient *rpadmin.AdminAPI) (*Client, error) {
	brokerAPI, err := kafkaAdminClient.ApiVersions(ctx)
	if err != nil {
		return nil, err
	}

	var scramAPISupported bool
	for _, api := range brokerAPI {
		_, _, supported := api.KeyVersions(kmsg.DescribeUserSCRAMCredentials.Int16())
		if supported {
			scramAPISupported = true
			break
		}
	}

	return &Client{
		client:            kubeClient,
		kafkaAdminClient:  kafkaAdminClient,
		adminClient:       adminClient,
		scramAPISupported: scramAPISupported,
		generator:         newPasswordGenerator(),
	}, nil
}

// Delete deletes the given user.
func (c *Client) Delete(ctx context.Context, user *redpandav1alpha2.User) error {
	return c.delete(ctx, user.Name)
}

// Create creates the given user, generating a password if necessary and synchronizing it to
// a Kubernetes secret.
func (c *Client) Create(ctx context.Context, user *redpandav1alpha2.User) error {
	password, err := c.getPassword(ctx, user)
	if err != nil {
		return err
	}

	sasl, err := user.Spec.Authentication.Type.ScramToKafka()
	if err != nil {
		return err
	}

	return c.create(ctx, user.Name, password, sasl)
}

// Has returns whether or not the Redpanda cluster already contains the given user.
func (c *Client) Has(ctx context.Context, user *redpandav1alpha2.User) (bool, error) {
	return c.has(ctx, user.Name)
}

// Close closes the underlying kafka connection
func (c *Client) Close() {
	c.kafkaAdminClient.Close()
	c.adminClient.Close()
}

func (c *Client) delete(ctx context.Context, username string) error {
	if c.scramAPISupported {
		resp, err := c.kafkaAdminClient.AlterUserSCRAMs(ctx, []kadm.DeleteSCRAM{{
			User: username,
		}}, nil)
		if err != nil {
			return err
		}
		return resp.Error()
	}

	return c.adminClient.DeleteUser(ctx, username)
}

func (c *Client) create(ctx context.Context, username, password string, mechanism kadm.ScramMechanism) error {
	if c.scramAPISupported {
		resp, err := c.kafkaAdminClient.AlterUserSCRAMs(ctx, nil, []kadm.UpsertSCRAM{{
			User:      username,
			Password:  password,
			Mechanism: mechanism,
		}})
		if err != nil {
			return err
		}
		return resp.Error()
	}

	return c.adminClient.CreateUser(ctx, username, password, mechanism.String())
}

func (c *Client) has(ctx context.Context, username string) (bool, error) {
	if c.scramAPISupported {
		scrams, err := c.kafkaAdminClient.DescribeUserSCRAMs(ctx, username)
		if err != nil {
			return false, err
		}
		if err := scrams.Error(); err != nil {
			return false, err
		}

		return len(scrams) == 0, nil
	}

	users, err := c.adminClient.ListUsers(ctx)
	if err != nil {
		return false, err
	}

	return slices.Contains(users, username), nil
}

func (c *Client) getPassword(ctx context.Context, user *redpandav1alpha2.User) (string, error) {
	auth := user.Spec.Authentication

	if auth == nil {
		return "", nil
	}

	userProvidedPassword := auth.Password.Value

	// if we have a ValueFrom, then use it, we'll:
	// 1. check if the Secret referenced exists
	// 2. check if the key exists for the given secret
	// 3. If it does, we return it
	// 4. If it doesn't we either dump the user provided
	//    password into the secret or dump a randomly
	//    generated password into the secret
	if auth.Password.ValueFrom != nil { //nolint:nestif // this is fine
		secret := auth.Password.ValueFrom.SecretKeyRef.Name
		key := auth.Password.ValueFrom.SecretKeyRef.Key
		if key == "" {
			key = "password"
		}

		var passwordSecret corev1.Secret
		nn := types.NamespacedName{Namespace: user.Namespace, Name: secret}
		if err := c.client.Get(ctx, nn, &passwordSecret); err != nil {
			if !apierrors.IsNotFound(err) {
				return "", err
			}

			return c.generateAndStorePassword(ctx, user, userProvidedPassword, nn, key)
		}

		data, ok := passwordSecret.Data[key]
		if !ok {
			return c.generateAndStorePassword(ctx, user, userProvidedPassword, nn, key)
		}

		return string(data), nil
	}

	return userProvidedPassword, nil
}

func (c *Client) generateAndStorePassword(ctx context.Context, user *redpandav1alpha2.User, password string, nn types.NamespacedName, key string) (string, error) {
	var err error

	if password == "" {
		// we weren't provided a password, so generate one
		password, err = c.generator.Generate()
		if err != nil {
			return "", err
		}
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: nn.Namespace,
			Name:      nn.Name,
		},
	}

	template := user.Spec.Template

	if _, err := controllerutil.CreateOrUpdate(ctx, c.client, secret, func() error {
		if secret.Data == nil {
			secret.Data = map[string][]byte{}
		}

		secret.Data[key] = []byte(password)
		if template != nil && template.Secret != nil {
			secret.ObjectMeta.Annotations = template.Secret.Metadata.Annotations
			secret.ObjectMeta.Labels = template.Secret.Metadata.Labels
		}
		// Set a controller reference so that when the user is deleted
		// the Secret is also GC'd.
		return controllerutil.SetControllerReference(user, secret, c.client.Scheme())
	}); err != nil {
		return "", err
	}

	return password, nil
}
