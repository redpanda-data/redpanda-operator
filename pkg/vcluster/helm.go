// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package vcluster

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	diskcached "k8s.io/client-go/discovery/cached/disk"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/client-go/util/homedir"
)

type vclusterRESTClientGetter struct {
	cluster *Cluster
}

// implementation of genericclioptions.RESTClientGetter

var overlyCautiousIllegalFileCharacters = regexp.MustCompile(`[^(\w/.)]`)

func getDefaultCacheDir() string {
	if kcd := os.Getenv("KUBECACHEDIR"); kcd != "" {
		return kcd
	}

	return filepath.Join(homedir.HomeDir(), ".kube", "cache")
}

func computeDiscoverCacheDir(parentDir, host string) string {
	// strip the optional scheme from host if its there:
	schemelessHost := strings.Replace(strings.Replace(host, "https://", "", 1), "http://", "", 1)
	// now do a simple collapse of non-AZ09 characters.  Collisions are possible but unlikely.  Even if we do collide the problem is short lived
	safeHost := overlyCautiousIllegalFileCharacters.ReplaceAllString(schemelessHost, "_")
	return filepath.Join(parentDir, safeHost)
}

func (c *vclusterRESTClientGetter) ToRESTConfig() (*rest.Config, error) {
	return c.cluster.config, nil
}

func (c *vclusterRESTClientGetter) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	cacheDir := getDefaultCacheDir()
	httpCacheDir := filepath.Join(cacheDir, "http")
	discoveryCacheDir := computeDiscoverCacheDir(filepath.Join(cacheDir, "discovery"), c.cluster.config.Host)

	return diskcached.NewCachedDiscoveryClientForConfig(c.cluster.config, discoveryCacheDir, httpCacheDir, time.Duration(6*time.Hour))
}

func (c *vclusterRESTClientGetter) ToRESTMapper() (meta.RESTMapper, error) {
	discoveryClient, err := c.ToDiscoveryClient()
	if err != nil {
		return nil, err
	}
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(discoveryClient)
	expander := restmapper.NewShortcutExpander(mapper, discoveryClient, func(a string) {})
	return expander, nil
}
func (c *vclusterRESTClientGetter) ToRawKubeConfigLoader() clientcmd.ClientConfig { return c }

// clientcmd.ClientConfig implementation

func (c *vclusterRESTClientGetter) Namespace() (string, bool, error) {
	return metav1.NamespaceDefault, false, nil
}

// the rest of these can be stubbed out
func (c *vclusterRESTClientGetter) ClientConfig() (*rest.Config, error) {
	return c.cluster.config, nil
}

func (c *vclusterRESTClientGetter) RawConfig() (clientcmdapi.Config, error) {
	return clientcmdapi.Config{}, nil
}

func (c *vclusterRESTClientGetter) ConfigAccess() clientcmd.ConfigAccess {
	return nil
}
