// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

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
