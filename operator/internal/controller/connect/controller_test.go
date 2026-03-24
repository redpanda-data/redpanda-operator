// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package connect

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/redpanda-data/common-go/license"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateLicenseNoPath(t *testing.T) {
	c := &Controller{
		LicenseFilePath: "",
	}
	err := c.validateLicense(nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no license configured")
}

func TestValidateLicenseBadPath(t *testing.T) {
	c := &Controller{
		LicenseFilePath: "/nonexistent/path/to/license",
	}
	err := c.validateLicense(nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read license")
}

func TestValidateLicenseInvalidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "license")
	require.NoError(t, os.WriteFile(path, []byte("not-a-valid-license"), 0o644))

	c := &Controller{
		LicenseFilePath: path,
	}
	err := c.validateLicense(nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read license")
}

func TestValidateLicenseOpenSource(t *testing.T) {
	// OpenSourceLicense does not allow enterprise features.
	l := license.OpenSourceLicense
	assert.False(t, l.AllowsEnterpriseFeatures())
}

func TestValidateLicenseExpired(t *testing.T) {
	err := license.CheckExpiration(time.Now().Add(-24 * time.Hour))
	require.Error(t, err)
}

func TestValidateLicenseNotExpired(t *testing.T) {
	err := license.CheckExpiration(time.Now().Add(24 * time.Hour))
	require.NoError(t, err)
}

func TestV0LicenseIncludesAllProducts(t *testing.T) {
	// V0 licenses include all products (returns true for any product).
	l := &license.V0RedpandaLicense{
		Type:   license.V0LicenseTypeEnterprise,
		Expiry: time.Now().Add(24 * time.Hour).Unix(),
	}
	assert.True(t, l.AllowsEnterpriseFeatures())
	assert.True(t, l.IncludesProduct(license.ProductConnect))
}

func TestV1LicenseWithConnectProduct(t *testing.T) {
	l := &license.V1RedpandaLicense{
		Type:     license.LicenseTypeEnterprise,
		Expiry:   time.Now().Add(24 * time.Hour).Unix(),
		Products: []license.Product{license.ProductConnect},
	}
	assert.True(t, l.AllowsEnterpriseFeatures())
	assert.True(t, l.IncludesProduct(license.ProductConnect))
}

func TestV1LicenseWithoutConnectProduct(t *testing.T) {
	l := &license.V1RedpandaLicense{
		Type:     license.LicenseTypeEnterprise,
		Expiry:   time.Now().Add(24 * time.Hour).Unix(),
		Products: []license.Product{}, // no products
	}
	assert.True(t, l.AllowsEnterpriseFeatures())
	assert.False(t, l.IncludesProduct(license.ProductConnect))
}

func TestV1TrialLicenseWithConnect(t *testing.T) {
	l := &license.V1RedpandaLicense{
		Type:     license.LicenseTypeFreeTrial,
		Expiry:   time.Now().Add(24 * time.Hour).Unix(),
		Products: []license.Product{license.ProductConnect},
	}
	assert.True(t, l.AllowsEnterpriseFeatures())
	assert.True(t, l.IncludesProduct(license.ProductConnect))
}

func TestV1ExpiredEnterpriseLicense(t *testing.T) {
	l := &license.V1RedpandaLicense{
		Type:     license.LicenseTypeEnterprise,
		Expiry:   time.Now().Add(-24 * time.Hour).Unix(),
		Products: []license.Product{license.ProductConnect},
	}
	// Expired license should not allow enterprise features.
	assert.False(t, l.AllowsEnterpriseFeatures())
}

func TestV1OpenSourceLicenseType(t *testing.T) {
	l := &license.V1RedpandaLicense{
		Type:     license.LicenseTypeOpenSource,
		Expiry:   time.Now().Add(24 * time.Hour).Unix(),
		Products: []license.Product{license.ProductConnect},
	}
	// Open source license type should not allow enterprise features.
	assert.False(t, l.AllowsEnterpriseFeatures())
}
