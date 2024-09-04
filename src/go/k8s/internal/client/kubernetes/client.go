// Package kubernetes holds logic for doing server side applies with a controller-runtime client.
package kubernetes

import (
	"encoding/json"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type applyPatch struct {
	applyConfiguration any
}

var _ client.Patch = (*applyPatch)(nil)

func ApplyPatch(applyConfiguration any) client.Patch {
	return &applyPatch{
		applyConfiguration: applyConfiguration,
	}
}

func (a *applyPatch) Type() types.PatchType {
	return types.ApplyPatchType
}

// Data returns the marshaled bytes of an underlying apply configuration
// rather than the object that is passed in.
func (a *applyPatch) Data(_ client.Object) ([]byte, error) {
	return json.Marshal(a.applyConfiguration)
}
