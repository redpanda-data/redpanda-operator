// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package decommissioning

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// the format logic for helm releases can be found:
// https://github.com/helm/helm/blob/2cea1466d3c27491364eb44bafc7be1ca5461b2d/pkg/storage/driver/util.go#L58

var gzipHeader = []byte{0x1f, 0x8b, 0x08}

// HelmFetcher fetches a Redpanda CR via initializing it virtually with a
// Helm values file stored in a secret. This is to maintain backwards
// compatibility with our current mechanism for decommissioning, but
// it should likely be dropped in the future with preference to using
// an RPK profile.
type HelmFetcher struct {
	client client.Client
}

var _ Fetcher = (*HelmFetcher)(nil)

func NewHelmFetcher(mgr ctrl.Manager) *HelmFetcher {
	return &HelmFetcher{client: mgr.GetClient()}
}

func (f *HelmFetcher) FetchLatest(ctx context.Context, name, namespace string) (any, error) {
	log := ctrl.LoggerFrom(ctx, "namespace", namespace, "name", name).WithName("HelmFetcher.FetchLatest")

	var secrets corev1.SecretList

	if err := f.client.List(ctx, &secrets, client.MatchingLabels{
		"name":  name,
		"owner": "helm",
	}, client.InNamespace(namespace)); err != nil {
		return nil, fmt.Errorf("fetching secrets list: %w", err)
	}

	latestVersion := 0
	var latestValues map[string]any
	for _, item := range secrets.Items {
		values, version, err := f.decode(item.Data["release"])
		if err != nil {
			// just log the error and move on rather than making it terminal
			// in case there's some secret that's just badly formatted
			log.Error(err, "decoding secret", "secret", item.Name)
			continue
		}
		if version > latestVersion {
			latestVersion = version
			latestValues = values
		}
	}

	if latestValues != nil {
		data, err := json.Marshal(latestValues)
		if err != nil {
			return nil, fmt.Errorf("marshaling values: %w", err)
		}

		cluster := &redpandav1alpha2.Redpanda{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: redpandav1alpha2.RedpandaSpec{ClusterSpec: &redpandav1alpha2.RedpandaClusterSpec{}},
		}

		if err := json.Unmarshal(data, cluster.Spec.ClusterSpec); err != nil {
			return nil, fmt.Errorf("unmarshaling values: %w", err)
		}

		return cluster, nil
	}

	err := errors.New("unable to find latest value")
	log.Error(err, "no secrets were decodable")
	return nil, err
}

type partialChart struct {
	Config  map[string]any `json:"config"`
	Version int            `json:"version"`
}

func (f *HelmFetcher) decode(data []byte) (map[string]any, int, error) {
	decoded := make([]byte, base64.StdEncoding.DecodedLen(len(data)))
	n, err := base64.StdEncoding.Decode(decoded, data)
	if err != nil {
		return nil, 0, err
	}
	decoded = decoded[:n]

	if len(decoded) > 3 && bytes.Equal(decoded[0:3], gzipHeader) {
		reader, err := gzip.NewReader(bytes.NewReader(decoded))
		if err != nil {
			return nil, 0, err
		}
		defer reader.Close()
		unzipped, err := io.ReadAll(reader)
		if err != nil {
			return nil, 0, err
		}
		decoded = unzipped
	}

	var chart partialChart
	if err := json.Unmarshal(decoded, &chart); err != nil {
		return nil, 0, err
	}

	// We only care about the chart.Config here and not the
	// merged values with the chart values because our
	// client initialization code already does the merging.
	return chart.Config, chart.Version, nil
}
