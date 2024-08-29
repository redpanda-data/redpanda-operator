package kubernetes

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type Client interface {
	client.Client
	ClearFinalizer(ctx context.Context, o client.Object, finalizer string) error
	Apply(ctx context.Context, o any, owner client.FieldOwner) error
	ApplyStatus(ctx context.Context, o any, owner client.FieldOwner) error
}

// KubernetesClient wraps a controller-runtime client, extending it with
// an Apply method that does server side apply with a typed ApplyConfiguration builder.
type KubernetesClient struct {
	client.Client
}

func Wrap(c client.Client) *KubernetesClient {
	return &KubernetesClient{
		Client: c,
	}
}

func (c *KubernetesClient) ClearFinalizer(ctx context.Context, o client.Object, finalizer string) error {
	if controllerutil.RemoveFinalizer(o, finalizer) {
		return c.Update(ctx, o)
	}

	return nil
}

func (c *KubernetesClient) Apply(ctx context.Context, o any, owner client.FieldOwner) error {
	var err error

	obj := &unstructured.Unstructured{}
	obj.Object, err = runtime.DefaultUnstructuredConverter.ToUnstructured(o)
	if err != nil {
		return err
	}

	return c.Patch(ctx, obj, client.Apply, client.ForceOwnership, owner)
}

func (c *KubernetesClient) ApplyStatus(ctx context.Context, o any, owner client.FieldOwner) error {
	var err error

	obj := &unstructured.Unstructured{}
	obj.Object, err = runtime.DefaultUnstructuredConverter.ToUnstructured(o)
	if err != nil {
		return err
	}

	fmt.Printf("%+v\n", obj.Object)
	return c.Status().Patch(ctx, obj, client.Apply, client.ForceOwnership, owner)
}
