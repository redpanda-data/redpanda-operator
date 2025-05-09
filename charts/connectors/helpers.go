// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_helpers.go.tpl
package connectors

import (
	"fmt"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

func Name(dot *helmette.Dot) string {
	values := helmette.Unwrap[Values](dot.Values)
	name := helmette.Default(dot.Chart.Name, values.NameOverride)
	return trunc(name)
}

func Fullname(dot *helmette.Dot) string {
	values := helmette.Unwrap[Values](dot.Values)

	if !helmette.Empty(values.FullnameOverride) {
		return trunc(values.FullnameOverride)
	}

	name := helmette.Default(dot.Chart.Name, values.NameOverride)

	if helmette.Contains(name, dot.Release.Name) {
		return trunc(dot.Release.Name)
	}
	return trunc(fmt.Sprintf("%s-%s", dot.Release.Name, name))
}

func FullLabels(dot *helmette.Dot) map[string]string {
	return helmette.Merge(map[string]string{
		"helm.sh/chart":                ChartLabels(dot),
		"app.kubernetes.io/managed-by": dot.Release.Service,
	}, PodLabels(dot))
}

func PodLabels(dot *helmette.Dot) map[string]string {
	values := helmette.Unwrap[Values](dot.Values)
	return helmette.Merge(map[string]string{
		"app.kubernetes.io/name":      Name(dot),
		"app.kubernetes.io/instance":  dot.Release.Name,
		"app.kubernetes.io/component": Name(dot),
	}, values.CommonLabels)
}

func ChartLabels(dot *helmette.Dot) string {
	chart := fmt.Sprintf("%s-%s", dot.Chart.Name, dot.Chart.Version)
	return trunc(helmette.Replace("+", "_", chart))
}

func Semver(dot *helmette.Dot) string {
	return helmette.TrimPrefix("v", Tag(dot))
}

func ServiceAccountName(dot *helmette.Dot) string {
	values := helmette.Unwrap[Values](dot.Values)
	if values.ServiceAccount.Create {
		return helmette.Default(Fullname(dot), values.ServiceAccount.Name)
	}
	return helmette.Default("default", values.ServiceAccount.Name)
}

func ServiceName(dot *helmette.Dot) string {
	values := helmette.Unwrap[Values](dot.Values)
	return helmette.Default(Fullname(dot), values.Service.Name)
}

func Tag(dot *helmette.Dot) string {
	values := helmette.Unwrap[Values](dot.Values)

	tag := helmette.Default(dot.Chart.AppVersion, values.Image.Tag)
	matchString := "^v(0|[1-9]\\d*)\\.(0|[1-9]\\d*)\\.(0|[1-9]\\d*)(?:-((?:0|[1-9]\\d*|\\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\\.(?:0|[1-9]\\d*|\\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\\+([0-9a-zA-Z-]+(?:\\.[0-9a-zA-Z-]+)*))?$"

	if !helmette.MustRegexMatch(matchString, tag) {
		// This error message is for end users. This can also occur if
		// AppVersion doesn't start with a 'v' in Chart.yaml.
		panic("image.tag must start with a 'v' and be a valid semver")
	}

	return tag
}

func trunc(s string) string {
	return helmette.TrimSuffix("-", helmette.Trunc(63, s))
}
