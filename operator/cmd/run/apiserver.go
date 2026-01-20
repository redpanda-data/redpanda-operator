// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package run

import (
	"fmt"
	"net"

	"github.com/redpanda-data/common-go/kube"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apivirtual "github.com/redpanda-data/redpanda-operator/operator/api/virtual"
	virtualv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/virtual/v1alpha1"
	virtual "github.com/redpanda-data/redpanda-operator/operator/internal/virtual"
	"github.com/redpanda-data/redpanda-operator/operator/internal/virtual/backends"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
)

type APIServerConfig struct {
	Manager    ctrl.Manager
	Factory    internalclient.ClientFactory
	ServiceKey types.NamespacedName
	SecretKey  types.NamespacedName
}

func SetupAPIServer(config APIServerConfig) error {
	rotator := kube.NewCertRotator(kube.CertRotatorConfig{
		DNSName:   fmt.Sprintf("%s.%s.svc", config.ServiceKey.Name, config.ServiceKey.Namespace),
		SecretKey: config.SecretKey,
		Service: &apiextensionsv1.ServiceReference{
			Name:      config.ServiceKey.Name,
			Namespace: config.ServiceKey.Namespace,
		},
		Webhooks: []kube.WebhookInfo{{
			Type: kube.APIService,
			Name: "v1alpha1.virtual.cluster.redpanda.com",
		}},
	})

	if err := kube.AddRotator(config.Manager, rotator); err != nil {
		return err
	}

	ctl, err := kube.FromRESTConfig(config.Manager.GetConfig(), kube.Options{
		Options: client.Options{
			Scheme: config.Manager.GetScheme(),
		},
	})
	if err != nil {
		return err
	}

	builder := kube.NewAPIServerManagedBy(config.Manager).WithRotator(rotator).WithBind(net.ParseIP("0.0.0.0"), 9050)
	builder.WithStorage("shadowlinks", virtual.NewVirtualStorage[apivirtual.ShadowLink, apivirtual.ShadowLinkList](
		// "virtual" shadowlinks
		[]string{"vsl"}, apivirtual.GroupVersion.WithResource("shadowlinks").GroupResource(), backends.NewShadowLinkBackend(config.Factory), ctl,
	))
	return builder.Complete(virtualv1alpha1.GroupVersion, virtualv1alpha1.GetOpenAPIDefinitions, "Virtual", "1.0")
}
