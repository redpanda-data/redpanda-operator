// Copyright 2026 Redpanda Data, Inc.
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
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
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
	sasl := kadm.ScramSha512
	if user.Spec.Authentication != nil && user.Spec.Authentication.Type != nil {
		var err error
		sasl, err = user.Spec.Authentication.Type.ScramToKafka()
		if err != nil {
			return err
		}
	}

	return c.delete(ctx, user.Name, sasl)
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

// Update re-reads the configured password and upserts the user's SCRAM
// credentials in Redpanda. Unlike Create, it never generates or stores a new
// password. This is used for ongoing credential sync when syncCredentials is
// enabled.
func (c *Client) Update(ctx context.Context, user *redpandav1alpha2.User) error {
	password, err := c.getExistingPassword(ctx, user)
	if err != nil {
		return err
	}

	sasl, err := user.Spec.Authentication.Type.ScramToKafka()
	if err != nil {
		return err
	}

	return c.create(ctx, user.Name, password, sasl)
}

// Has returns whether or not the Redpanda cluster already contains the given
// user, regardless of which SASL mechanism its credential uses. This is the
// right question for deletion: the user is present, and therefore needs
// cleaning up, as long as a credential exists under its name.
func (c *Client) Has(ctx context.Context, user *redpandav1alpha2.User) (bool, error) {
	exists, _, err := c.describe(ctx, user.Name)
	return exists, err
}

// CredentialState is what a Redpanda cluster currently holds for a user.
type CredentialState struct {
	// Exists reports whether the cluster holds a credential for the user's name
	// under any SASL mechanism. This is the right question for deletion.
	Exists bool
	// HasRequestedMechanism reports whether the user's credential uses the
	// mechanism named by spec.authentication.type. This is the right question
	// for deciding whether the credential needs to be written.
	//
	// Redpanda stores a single SCRAM credential per user, mechanism included, so
	// a user whose requested mechanism changed still Exists -- by way of a
	// credential for the *previous* mechanism -- while being unable to
	// authenticate with the mechanism it now asks for. That stale credential is
	// not a redundant extra: it is the only one the user has.
	//
	// Brokers that predate the Kafka SCRAM APIs do not expose the mechanism.
	// There this mirrors Exists, preserving the previous behavior rather than
	// guessing.
	HasRequestedMechanism bool
}

// CredentialState returns what the cluster holds for the given user, in a single
// round trip.
func (c *Client) CredentialState(ctx context.Context, user *redpandav1alpha2.User) (CredentialState, error) {
	exists, mechanisms, err := c.describe(ctx, user.Name)
	if err != nil {
		return CredentialState{}, err
	}

	if !exists {
		return CredentialState{}, nil
	}

	// A nil mechanism list means "unknown" rather than "none": either the broker
	// does not report mechanisms, or the user requests no particular one.
	// Neither is grounds for rewriting credentials.
	if mechanisms == nil || user.Spec.Authentication == nil || user.Spec.Authentication.Type == nil {
		return CredentialState{Exists: true, HasRequestedMechanism: true}, nil
	}

	mechanism, err := user.Spec.Authentication.Type.ScramToKafka()
	if err != nil {
		return CredentialState{}, err
	}

	return CredentialState{
		Exists:                true,
		HasRequestedMechanism: slices.Contains(mechanisms, mechanism),
	}, nil
}

// Close closes the underlying kafka connection
func (c *Client) Close() {
	c.kafkaAdminClient.Close()
	c.adminClient.Close()
}

func (c *Client) delete(ctx context.Context, username string, mechanism kadm.ScramMechanism) error {
	if c.scramAPISupported {
		resp, err := c.kafkaAdminClient.AlterUserSCRAMs(ctx, []kadm.DeleteSCRAM{{
			User:      username,
			Mechanism: mechanism,
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
			// The Iteration is hardcoded to the same as Admin API would create SCRAM user.
			Iterations: 4096,
		}})
		if err != nil {
			return err
		}
		return resp.Error()
	}

	mechanismName := mechanism.String()
	if err := c.adminClient.CreateUser(ctx, username, password, mechanismName); err != nil {
		if strings.Contains(err.Error(), "already exists") {
			return c.adminClient.UpdateUser(ctx, username, password, mechanismName)
		}
		return err
	}
	return nil
}

func (c *Client) getExistingPassword(ctx context.Context, user *redpandav1alpha2.User) (string, error) {
	auth := user.Spec.Authentication
	if auth == nil {
		return "", nil
	}

	if auth.Password.ValueFrom == nil {
		return auth.Password.Value, nil
	}

	secret := auth.Password.ValueFrom.SecretKeyRef.Name
	key := auth.Password.ValueFrom.SecretKeyRef.Key
	if key == "" {
		key = "password"
	}

	var passwordSecret corev1.Secret
	nn := types.NamespacedName{Namespace: user.Namespace, Name: secret}
	if err := c.client.Get(ctx, nn, &passwordSecret); err != nil {
		return "", err
	}

	data, ok := passwordSecret.Data[key]
	if !ok {
		return "", errors.Newf("key %q not found in Secret %s/%s", key, user.Namespace, secret)
	}

	return string(data), nil
}

// describe reports whether the given user exists and, when the broker supports
// the Kafka SCRAM APIs, which SASL mechanisms its credentials use. A nil
// mechanism slice means the mechanisms are unknown, which is distinct from a
// non-nil empty slice meaning the user holds no credentials.
//
// The Kafka API models this as a list, but Redpanda stores a single credential
// per user, so in practice at most one mechanism is reported.
func (c *Client) describe(ctx context.Context, username string) (bool, []kadm.ScramMechanism, error) {
	if c.scramAPISupported {
		scrams, err := c.kafkaAdminClient.DescribeUserSCRAMs(ctx, username)
		if err != nil {
			return false, nil, err
		}
		if err := scrams.Error(); err != nil {
			var franzErr *kerr.Error
			if errors.As(err, &franzErr) {
				if franzErr.Code == kerr.ResourceNotFound.Code {
					return false, nil, nil
				}
			}

			return false, nil, err
		}

		described, ok := scrams[username]
		if !ok {
			return len(scrams) != 0, nil, nil
		}

		mechanisms := make([]kadm.ScramMechanism, 0, len(described.CredInfos))
		for _, info := range described.CredInfos {
			mechanisms = append(mechanisms, info.Mechanism)
		}

		return len(scrams) != 0, mechanisms, nil
	}

	// The admin API only lists user names, so mechanisms are unknowable here.
	users, err := c.adminClient.ListUsers(ctx)
	if err != nil {
		return false, nil, err
	}

	return slices.Contains(users, username), nil, nil
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
	//    generated password into the secret or return
	//    an error if NoGenerate flag is set.
	if auth.Password.ValueFrom != nil { //nolint:nestif // this is fine
		secret := auth.Password.ValueFrom.SecretKeyRef.Name
		key := auth.Password.ValueFrom.SecretKeyRef.Key
		if key == "" {
			key = "password"
		}

		var passwordSecret corev1.Secret
		nn := types.NamespacedName{Namespace: user.Namespace, Name: secret}
		if err := c.client.Get(ctx, nn, &passwordSecret); err != nil {
			if !apierrors.IsNotFound(err) || auth.Password.NoGenerate {
				return "", err
			}

			return c.generateAndStorePassword(ctx, user, userProvidedPassword, nn, key)
		}

		data, ok := passwordSecret.Data[key]
		if !ok {
			if auth.Password.NoGenerate {
				return "", errors.Newf("key %q not found in Secret %s/%s", key, user.Namespace, secret)
			}

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
