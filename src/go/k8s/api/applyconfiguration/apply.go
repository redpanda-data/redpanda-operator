package applyconfiguration

import (
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ApplyConfigurationObject is a special object used for server side apply. It is a
// Kubernetes object with all fields set as pointers to distinguish between nil value and empty.
// It implements the getters for Name and Namespace for use with with ApplyPatch.
type ApplyConfigurationObject interface {
	GetNamespace() string
	GetName() string
}

// applyFromPatch uses server-side-apply with a ApplyConfiguration object.
type applyFromPatch struct {
	patch ApplyConfigurationObject
}

// Type implements Patch.
func (p applyFromPatch) Type() types.PatchType {
	return types.ApplyPatchType
}

// setNameNamespace sets name and namespace for the returned object if an ApplyConfiguration was supplied.
func setNameNamespace(obj client.Object, ac ApplyConfigurationObject) error {
	if ac.GetName() == "" {
		return fmt.Errorf("ApplyConfiguration must have a non-empty Name")
	}
	obj.SetName(ac.GetName())
	obj.SetNamespace(ac.GetNamespace())
	return nil
}

// Data implements Patch.
func (p applyFromPatch) Data(o client.Object) ([]byte, error) {
	// If an ApplyConfiguration is provided, use that as the data to send
	if p.patch == nil {
		return nil, fmt.Errorf("ApplyConfiguration must be specified when using ApplyFrom Patch")
	}

	if err := setNameNamespace(o, p.patch); err != nil {
		return nil, err
	}

	return json.Marshal(p.patch)
}

// ApplyFrom creates an applyFromPatch object with the provided ApplyConfiguration.
// Usage:
//
//	var owner client.FieldOwner
//	var c client.Client
//	config := acv1alpha2.User(o.GetName(), o.GetNamespace()).WithFinalizers("myfinalizer")
//	c.Patch(ctx, o, ac.ApplyFrom(config), client.ForceOwnership, owner)
func ApplyFrom(ac ApplyConfigurationObject) client.Patch {
	return applyFromPatch{patch: ac}
}
