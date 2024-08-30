// Package kubernetes holds logic for doing server side applies with a controller-runtime client.
package kubernetes

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// Client adds some helper methods to a standard controller runtime client.
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

// Wrap wraps a standard controller runtime client with some helpers for server-side apply
// of *ApplyConfiguration objects.
func Wrap(c client.Client) *KubernetesClient {
	return &KubernetesClient{
		Client: c,
	}
}

// ClearFinalizer clears a finalizer from the given object. Because finalizers are generally cleared
// only when a resource is no longer managed, this should be the final step in any reconciliation loop.
// Additionally, due to finalizers being annotated with patchStrategy=merge, we can't remove a finalizer
// via a typical server-side apply, so instead we send a full Update to the api server.
func (c *KubernetesClient) ClearFinalizer(ctx context.Context, o client.Object, finalizer string) error {
	if controllerutil.RemoveFinalizer(o, finalizer) {
		return c.Update(ctx, o)
	}

	return nil
}

// Apply issues a server-side apply request using conversion routines to an unstructured object.
// Due to the limitations of generated ApplyConfiguration objects not having a consistent interface
// we pass an arbitrary interface as an argument here. That said, callers should only use this function
// with ApplyConfiguration objects.
func (c *KubernetesClient) Apply(ctx context.Context, o any, owner client.FieldOwner) error {
	var err error

	obj := &unstructured.Unstructured{}
	obj.Object, err = runtime.DefaultUnstructuredConverter.ToUnstructured(o)
	if err != nil {
		return err
	}

	return c.Patch(ctx, obj, client.Apply, client.ForceOwnership, owner)
}

// ApplyStatus issues a server-side apply request for a status subresource using conversion routines
// to an unstructured object. Due to the limitations of generated ApplyConfiguration objects not having
// a consistent interface we pass an arbitrary interface as an argument here. That said, callers should
// only use this function with ApplyConfiguration objects.
func (c *KubernetesClient) ApplyStatus(ctx context.Context, o any, owner client.FieldOwner) error {
	var err error

	obj := &unstructured.Unstructured{}
	obj.Object, err = runtime.DefaultUnstructuredConverter.ToUnstructured(o)
	if err != nil {
		return err
	}

	return c.Status().Patch(ctx, obj, client.Apply, client.ForceOwnership, owner)
}
