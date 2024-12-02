// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package mutability

import "github.com/redpanda-data/redpanda-operator/helm-charts/pkg/gotohelm/helmette"

type Values struct {
	Name       string
	Labels     map[string]string
	SubService *SubService
}

type SubService struct {
	Name   string
	Labels map[string]string
}

func Mutability() map[string]any {
	var v Values
	v.Labels = map[string]string{}
	v.SubService = &SubService{}
	v.SubService.Labels = map[string]string{}

	v.SubService.Name = "Hello!"
	v.SubService.Labels["hello"] = "world"

	return map[string]any{
		"values": helmette.MustFromJSON(helmette.MustToJSON(v)),
	}
}
