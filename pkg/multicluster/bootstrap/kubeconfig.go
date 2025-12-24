// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package bootstrap

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"text/template"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	defaultServiceAccountNamespace = metav1.NamespaceDefault
	defaultServiceAccountName      = "multicluster-operator"
)

type RemoteKubernetesConfiguration struct {
	ContextName     string
	Namespace       string
	Name            string
	APIServer       string
	RESTConfig      *rest.Config
	EnsureNamespace bool
}

func configFromContext(contextName string) (*rest.Config, error) {
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{CurrentContext: contextName},
	).ClientConfig()
}

func CreateKubeconfigSecret(ctx context.Context, data []byte, configuration *RemoteKubernetesConfiguration) error {
	if configuration == nil {
		configuration = &RemoteKubernetesConfiguration{}
	}
	if configuration.Namespace == "" {
		configuration.Namespace = defaultServiceAccountNamespace
	}
	if configuration.Name == "" {
		configuration.Name = defaultServiceAccountName
	}
	if configuration.RESTConfig == nil {
		if configuration.ContextName == "" {
			return errors.New("either the name of a kubernetes context or a rest config must be specified")
		}
		config, err := configFromContext(configuration.ContextName)
		if err != nil {
			return fmt.Errorf("getting REST configuration: %v", err)
		}
		configuration.RESTConfig = config
	}

	cl, err := client.New(configuration.RESTConfig, client.Options{})
	if err != nil {
		return fmt.Errorf("initializing client: %v", err)
	}

	if configuration.EnsureNamespace {
		if err := EnsureNamespace(ctx, configuration.Namespace, cl); err != nil {
			return fmt.Errorf("ensuring namespace exists: %v", err)
		}
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configuration.Name + "-kubeconfig",
			Namespace: configuration.Namespace,
		},
		Data: map[string][]byte{
			"kubeconfig.yaml": data,
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, cl, secret, func() error {
		secret.Data = map[string][]byte{
			"kubeconfig.yaml": data,
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("creating kubeconfig file: %v", err)
	}
	return nil
}

func CreateRemoteKubeconfig(ctx context.Context, configuration *RemoteKubernetesConfiguration) ([]byte, error) {
	if configuration == nil {
		configuration = &RemoteKubernetesConfiguration{}
	}
	if configuration.Namespace == "" {
		configuration.Namespace = defaultServiceAccountNamespace
	}
	if configuration.Name == "" {
		configuration.Name = defaultServiceAccountName
	}
	if configuration.RESTConfig == nil {
		if configuration.ContextName == "" {
			return nil, errors.New("either the name of a kubernetes context or a rest config must be specified")
		}
		config, err := configFromContext(configuration.ContextName)
		if err != nil {
			return nil, fmt.Errorf("getting REST configuration: %v", err)
		}
		configuration.RESTConfig = config
	}
	if configuration.APIServer == "" {
		configuration.APIServer = configuration.RESTConfig.Host
	}

	cl, err := client.New(configuration.RESTConfig, client.Options{})
	if err != nil {
		return nil, fmt.Errorf("initializing client: %v", err)
	}

	if configuration.EnsureNamespace {
		if err := EnsureNamespace(ctx, configuration.Namespace, cl); err != nil {
			return nil, fmt.Errorf("ensuring namespace exists: %v", err)
		}
	}

	_, err = controllerutil.CreateOrUpdate(ctx, cl, &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configuration.Name,
			Namespace: configuration.Namespace,
		},
	}, func() error { return nil })
	if err != nil {
		return nil, fmt.Errorf("creating service account: %v", err)
	}

	_, err = controllerutil.CreateOrUpdate(ctx, cl, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configuration.Name + "-token",
			Namespace: configuration.Namespace,
			Annotations: map[string]string{
				corev1.ServiceAccountNameKey: configuration.Name,
			},
		},
		Type: corev1.SecretTypeServiceAccountToken,
	}, func() error { return nil })
	if err != nil {
		return nil, fmt.Errorf("creating service account token: %v", err)
	}

	trigger := time.After(0)
	for {
		select {
		case <-trigger:
			var token corev1.Secret
			if err := cl.Get(ctx, types.NamespacedName{
				Namespace: configuration.Namespace,
				Name:      configuration.Name + "-token",
			}, &token); err != nil {
				return nil, err
			}
			data := token.Data
			if data != nil {
				token, hasToken := data["token"]
				certificate, hasCertificate := data["ca.crt"]
				if hasToken && hasCertificate {
					var buf bytes.Buffer
					err = kubeconfigTemplate.Execute(&buf, struct {
						CA        string
						APIServer string
						Cluster   string
						User      string
						Token     string
					}{
						CA:        base64.StdEncoding.EncodeToString(certificate),
						APIServer: configuration.APIServer,
						Token:     string(token),
						User:      configuration.Name,
						Cluster:   configuration.ContextName,
					})
					if err != nil {
						return nil, fmt.Errorf("creating kubeconfig file: %v", err)
					}
					return buf.Bytes(), nil
				}
			}
			trigger = time.After(1 * time.Second)
		case <-ctx.Done():
			return nil, fmt.Errorf("unable to get populated token")
		}
	}
}

var kubeconfigTemplate = template.Must(template.New("").Parse(`
apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: {{.CA}}
    server: {{.APIServer}}
  name: {{.Cluster}}
contexts:
- context:
    cluster: {{.Cluster}}
    user: {{.User}}
  name: {{.Cluster}}
current-context: {{.Cluster}}
kind: Config
preferences: {}
users:
- name: {{.User}}
  user:
    token: {{.Token}}
`))

func EnsureNamespace(ctx context.Context, namespace string, cl client.Client) error {
	_, err := controllerutil.CreateOrUpdate(ctx, cl, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}, func() error { return nil })
	return err
}
