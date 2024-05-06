package v1alpha1_test

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"

	fuzz "github.com/google/gofuzz"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/src/go/k8s/api/redpanda/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/src/go/k8s/api/redpanda/v1alpha2"
)

func TestConvertFrom(t *testing.T) {
	for _, tc := range []struct {
		flag *v1alpha2.CloudStorageEnabledBool
		name string
	}{
		{nil, "nil"},
		{ptr.To(v1alpha2.CloudStorageEnabledBool(true)), "enabled"},
		{ptr.To(v1alpha2.CloudStorageEnabledBool(false)), "disabled"},
	} {
		t.Run(fmt.Sprintf("cloud-enabled-%s", tc.name), func(t *testing.T) {
			newR := createNewRedpanda(tc.flag)
			newRJSON, err := json.Marshal(newR)
			require.NoError(t, err)

			oldR := v1alpha1.Redpanda{}
			err = oldR.ConvertFrom(newR)
			require.NoError(t, err)

			oldRJSON, err := json.Marshal(oldR)
			require.NoError(t, err)

			oldRJSONStr := strings.ReplaceAll(string(oldRJSON), "\"cloud_storage_enabled\":\"true\"", "\"cloud_storage_enabled\":true")
			oldRJSONStr = strings.ReplaceAll(oldRJSONStr, "\"cloud_storage_enabled\":\"false\"", "\"cloud_storage_enabled\":false")

			require.JSONEq(t, string(newRJSON), oldRJSONStr)
		})
	}
}

func TestConvertTo(t *testing.T) {
	random := ""

	unicodeRange := fuzz.UnicodeRange{First: 'a', Last: 'z'}

	f := fuzz.New().NilChance(0).
		NumElements(0, 1).
		Funcs(unicodeRange.CustomStringFuzzFunc())
	f.Fuzz(&random)

	for _, tc := range []struct {
		name string
		flag *v1alpha1.CloudStorageEnabledString
	}{
		{"nil", nil},
		{"enabled", ptr.To(v1alpha1.CloudStorageEnabledString("true"))},
		{"disabled", ptr.To(v1alpha1.CloudStorageEnabledString("false"))},
		{"random", ptr.To(v1alpha1.CloudStorageEnabledString(random))},
	} {
		t.Run(fmt.Sprintf("cloud-enabled-%s", tc.name), func(t *testing.T) {
			newR := &v1alpha2.Redpanda{}

			oldR := createOldRedpanda(tc.flag)
			err := oldR.ConvertTo(newR)
			require.NoError(t, err)

			oldRJSON, err := json.Marshal(oldR)
			require.NoError(t, err)

			newRJSON, err := json.Marshal(newR)
			require.NoError(t, err)

			oldRJSONStr := strings.ReplaceAll(string(oldRJSON), "\"cloud_storage_enabled\":\"true\"", "\"cloud_storage_enabled\":true")
			oldRJSONStr = strings.ReplaceAll(oldRJSONStr, "\"cloud_storage_enabled\":\"false\"", "\"cloud_storage_enabled\":false")
			if tc.flag != nil {
				oldRJSONStr = strings.ReplaceAll(oldRJSONStr, fmt.Sprintf("\"cloud_storage_enabled\":\"%s\"", *tc.flag), "\"cloud_storage_enabled\":false")
			}

			require.JSONEq(t, string(newRJSON), oldRJSONStr)
		})
	}
}

func createNewRedpanda(flag *v1alpha2.CloudStorageEnabledBool) *v1alpha2.Redpanda { // nolint:dupl // v1alpha1 is not the same as v1alpha2
	unicodeRange := fuzz.UnicodeRange{First: 'a', Last: 'z'}

	f := fuzz.New().NilChance(0).
		NumElements(0, 1).
		Funcs(unicodeRange.CustomStringFuzzFunc()).
		SkipFieldsWithPattern(regexp.MustCompile("Console|Config|Connectors|FieldsV1"))

	r := &v1alpha2.Redpanda{
		Spec: v1alpha2.RedpandaSpec{
			ClusterSpec: &v1alpha2.RedpandaClusterSpec{
				Storage: &v1alpha2.Storage{
					Tiered: &v1alpha2.Tiered{},
				},
			},
		},
	}

	for {
		f.Fuzz(r)

		if _, err := json.Marshal(r); err == nil {
			break
		}
	}

	r.Spec.ClusterSpec.Storage.Tiered.Config = &v1alpha2.TieredConfig{
		CloudStorageEnabled: flag,
	}

	return r
}

func createOldRedpanda(flag *v1alpha1.CloudStorageEnabledString) *v1alpha1.Redpanda { // nolint:dupl // v1alpha1 is not the same as v1alpha2
	unicodeRange := fuzz.UnicodeRange{First: 'a', Last: 'z'}

	f := fuzz.New().NilChance(0).
		NumElements(0, 1).
		Funcs(unicodeRange.CustomStringFuzzFunc()).
		SkipFieldsWithPattern(regexp.MustCompile("Console|Config|Connectors|FieldsV1"))

	r := &v1alpha1.Redpanda{
		Spec: v1alpha1.RedpandaSpec{
			ClusterSpec: &v1alpha1.RedpandaClusterSpec{
				Storage: &v1alpha1.Storage{
					Tiered: &v1alpha1.Tiered{},
				},
			},
		},
	}

	for {
		f.Fuzz(r)

		if _, err := json.Marshal(r); err == nil {
			break
		}
	}

	r.Spec.ClusterSpec.Storage.Tiered.Config = &v1alpha1.TieredConfig{
		CloudStorageEnabled: flag,
	}
	return r
}
