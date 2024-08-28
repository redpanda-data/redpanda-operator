package client

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/redpanda-data/common-go/rpadmin"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/src/go/k8s/api/redpanda/v1alpha2"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// UserClient is a wrapper around user/ACL creation for a v1alpha2.User
type UserClient interface {
	// HasUser checks if the user associated with this client exists.
	HasUser(ctx context.Context) (bool, error)
	// ListACLs returns all of ACLs for the user associated with this client.
	ListACLs(ctx context.Context) ([]kmsg.DescribeACLsResponseResource, error)
	// CreateACLs creates the given ACLs for the user associated with this client.
	CreateACLs(ctx context.Context, acls []kmsg.CreateACLsRequestCreation) error
	// DeleteACLs deletes the ACLs matching the given filters for the user associated with this client.
	DeleteACLs(ctx context.Context, deletions []kmsg.DeleteACLsRequestFilter) error
	// DeleteAllACLs deletes all of the ACLs for the user associated with this client.
	DeleteAllACLs(ctx context.Context) error
	// CreateUser creates the user associated with this client.
	CreateUser(ctx context.Context) error
	// DeleteUser deletes the user associated with this client.
	DeleteUser(ctx context.Context) error
	// SyncACLs synchronizes all the ACLs specified by a v1alpha2.User, deleting an creating
	// ACLs as necessary.
	SyncACLs(ctx context.Context) (int, int, error)
}

type userClient struct {
	user              *redpandav1alpha2.User
	client            client.Client
	kafkaClient       *kgo.Client
	kafkaAdminClient  *kadm.Client
	adminClient       *rpadmin.AdminAPI
	generator         *passwordGenerator
	scramAPISupported bool
}

var _ UserClient = (*userClient)(nil)

func (c *Factory) UserClient(ctx context.Context, user *redpandav1alpha2.User, opts ...kgo.Opt) (UserClient, error) {
	kafkaClient, err := c.KafkaClient(ctx, user, opts...)
	if err != nil {
		return nil, err
	}

	adminClient, err := c.RedpandaAdminClient(ctx, user)
	if err != nil {
		return nil, err
	}

	kafkaAdminClient := kadm.NewClient(kafkaClient)
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

	return &userClient{
		user:              user,
		client:            c.Client,
		kafkaClient:       kafkaClient,
		kafkaAdminClient:  kafkaAdminClient,
		adminClient:       adminClient,
		scramAPISupported: scramAPISupported,
		generator:         newPasswordGenerator(),
	}, nil
}

func (c *userClient) username() string {
	return c.user.Name
}

func (c *userClient) userACLName() string {
	return c.user.ACLName()
}

func (c *userClient) HasUser(ctx context.Context) (bool, error) {
	if c.scramAPISupported {
		scrams, err := c.kafkaAdminClient.DescribeUserSCRAMs(ctx, c.username())
		if err != nil {
			return false, err
		}
		return len(scrams) == 0, nil
	}

	users, err := c.adminClient.ListUsers(ctx)
	if err != nil {
		return false, err
	}

	return slices.Contains(users, c.username()), nil
}

func (c *userClient) ListACLs(ctx context.Context) ([]kmsg.DescribeACLsResponseResource, error) {
	ptrUsername := kmsg.StringPtr(c.userACLName())

	req := kmsg.NewPtrDescribeACLsRequest()
	req.PermissionType = kmsg.ACLPermissionTypeAny
	req.ResourceType = kmsg.ACLResourceTypeAny
	req.Principal = ptrUsername
	req.Operation = kmsg.ACLOperationAny

	response, err := req.RequestWith(ctx, c.kafkaClient)
	if err != nil {
		return nil, err
	}
	if response.ErrorMessage != nil {
		return nil, errors.New(*response.ErrorMessage)
	}

	// we have an error code but no error message
	if response.ErrorCode != 0 {
		return nil, fmt.Errorf("error code: %d", response.ErrorCode)
	}

	return response.Resources, nil
}

func (c *userClient) CreateACLs(ctx context.Context, acls []kmsg.CreateACLsRequestCreation) error {
	if len(acls) == 0 {
		return nil
	}

	req := kmsg.NewPtrCreateACLsRequest()
	req.Creations = acls

	creation, err := req.RequestWith(ctx, c.kafkaClient)
	if err != nil {
		return err
	}

	for _, result := range creation.Results {
		if result.ErrorMessage != nil {
			return errors.New(*result.ErrorMessage)
		}

		// we have an error code but no error message
		if result.ErrorCode != 0 {
			return fmt.Errorf("error code: %d", result.ErrorCode)
		}
	}

	return nil
}

func (c *userClient) DeleteACLs(ctx context.Context, deletions []kmsg.DeleteACLsRequestFilter) error {
	if len(deletions) == 0 {
		return nil
	}

	req := kmsg.NewPtrDeleteACLsRequest()
	req.Filters = deletions

	response, err := req.RequestWith(ctx, c.kafkaClient)
	if err != nil {
		return err
	}

	for _, result := range response.Results {
		if result.ErrorMessage != nil {
			return errors.New(*result.ErrorMessage)
		}
	}

	return nil
}

func (c *userClient) DeleteAllACLs(ctx context.Context) error {
	ptrUsername := kmsg.StringPtr(c.userACLName())

	req := kmsg.NewPtrDeleteACLsRequest()
	req.Filters = []kmsg.DeleteACLsRequestFilter{{
		PermissionType:      kmsg.ACLPermissionTypeAny,
		ResourceType:        kmsg.ACLResourceTypeAny,
		ResourcePatternType: kmsg.ACLResourcePatternTypeAny,
		Principal:           ptrUsername,
		Operation:           kmsg.ACLOperationAny,
	}}

	response, err := req.RequestWith(ctx, c.kafkaClient)
	if err != nil {
		return err
	}

	for _, result := range response.Results {
		if result.ErrorMessage != nil {
			return errors.New(*result.ErrorMessage)
		}

		// we have an error code but no error message
		if result.ErrorCode != 0 {
			return fmt.Errorf("error code: %d", result.ErrorCode)
		}
	}

	return nil
}

func (c *userClient) getPassword(ctx context.Context) (string, error) {
	auth := c.user.Spec.Authentication

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
	if auth.Password.ValueFrom != nil {
		secret := auth.Password.ValueFrom.SecretKeyRef.Name
		key := auth.Password.ValueFrom.SecretKeyRef.Key
		if key == "" {
			key = "password"
		}

		var passwordSecret corev1.Secret
		nn := types.NamespacedName{Namespace: c.user.Namespace, Name: secret}
		if err := c.client.Get(ctx, nn, &passwordSecret); err != nil {
			if !apierrors.IsNotFound(err) {
				return "", err
			}

			return c.generateAndStorePassword(ctx, userProvidedPassword, nn, key)
		}

		data, ok := passwordSecret.Data[key]
		if !ok {
			return c.generateAndStorePassword(ctx, userProvidedPassword, nn, key)
		}

		return string(data), nil
	}

	return userProvidedPassword, nil
}

func (c *userClient) generateAndStorePassword(ctx context.Context, password string, nn types.NamespacedName, key string) (string, error) {
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

	template := c.user.Spec.Template

	if _, err := controllerutil.CreateOrUpdate(ctx, c.client, secret, func() error {
		secret.Data[key] = []byte(password)
		if template != nil && template.Secret != nil {
			secret.ObjectMeta.Annotations = template.Secret.Metadata.Annotations
			secret.ObjectMeta.Labels = template.Secret.Metadata.Labels
		}
		return controllerutil.SetControllerReference(c.user, secret, c.client.Scheme())
	}); err != nil {
		return "", err
	}

	return password, nil
}

func (c *userClient) CreateUser(ctx context.Context) error {
	password, err := c.getPassword(ctx)
	if err != nil {
		return err
	}

	sasl, err := c.user.Spec.Authentication.Type.ScramToKafka()
	if err != nil {
		return err
	}

	if c.scramAPISupported {
		_, err = c.kafkaAdminClient.AlterUserSCRAMs(ctx, nil, []kadm.UpsertSCRAM{{
			User:      c.username(),
			Password:  password,
			Mechanism: sasl,
		}})
		return err
	}

	return c.adminClient.CreateUser(ctx, c.username(), password, sasl.String())
}

func (c *userClient) DeleteUser(ctx context.Context) error {
	if c.scramAPISupported {
		_, err := c.kafkaAdminClient.AlterUserSCRAMs(ctx, []kadm.DeleteSCRAM{{
			User: c.username(),
		}}, nil)
		return err
	}

	return c.adminClient.DeleteUser(ctx, c.username())
}

func (c *userClient) SyncACLs(ctx context.Context) (int, int, error) {
	acls, err := c.ListACLs(ctx)
	if err != nil {
		return 0, 0, err
	}

	creations, deletions, err := calculateACLs(c.user, acls)
	if err != nil {
		return 0, 0, err
	}

	if err := c.CreateACLs(ctx, creations); err != nil {
		return 0, 0, err
	}
	if err := c.DeleteACLs(ctx, deletions); err != nil {
		return 0, 0, err
	}

	return len(creations), len(deletions), nil
}
