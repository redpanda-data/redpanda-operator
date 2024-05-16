// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha1

import (
	"encoding/json"
	"fmt"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/conversion"

	v1alpha2 "github.com/redpanda-data/redpanda-operator/src/go/k8s/api/redpanda/v1alpha2"
)

var _ conversion.Convertible = &Redpanda{}

// SetupWebhookWithManager will setup the manager to manage the webhooks
func (r *Redpanda) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// ConvertTo converts Redpanda to the Hub version (v1alpha2).
func (src *Redpanda) ConvertTo(dstRaw conversion.Hub) error { // nolint:stylecheck // `src` as a received helps with readability
	dst := dstRaw.(*v1alpha2.Redpanda)

	b, err := json.Marshal(src)
	if err != nil {
		return fmt.Errorf("marshaling %T: %w", Redpanda{}, err)
	}

	if err = json.Unmarshal(b, dst); err != nil {
		return fmt.Errorf("unmarshaling %T: %w", v1alpha2.Redpanda{}, err)
	}

	return nil
}

// ConvertFrom converts from the Hub version (v1) to this version.
func (dst *Redpanda) ConvertFrom(srcRaw conversion.Hub) error { // nolint:stylecheck // `dst` as a received helps with readability
	src := srcRaw.(*v1alpha2.Redpanda)

	b, err := json.Marshal(src)
	if err != nil {
		return fmt.Errorf("marshaling %T: %w", v1alpha2.Redpanda{}, err)
	}

	if err = json.Unmarshal(b, dst); err != nil {
		return fmt.Errorf("unmarshaling %T: %w", Redpanda{}, err)
	}
	return nil
}
