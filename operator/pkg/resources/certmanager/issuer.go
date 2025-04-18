// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package certmanager

import (
	"context"
	"fmt"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/labels"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources"
)

var _ resources.Resource = &IssuerResource{}

// IssuerResource is part of the reconciliation of redpanda.vectorized.io CRD
// creating certificate issuer when TLS is enabled
type IssuerResource struct {
	k8sclient.Client
	scheme       *runtime.Scheme
	pandaCluster *vectorizedv1alpha1.Cluster
	key          types.NamespacedName
	secretName   string
	logger       logr.Logger
}

// NewIssuer creates IssuerResource
func NewIssuer(
	client k8sclient.Client,
	scheme *runtime.Scheme,
	pandaCluster *vectorizedv1alpha1.Cluster,
	key types.NamespacedName,
	secretName string,
	logger logr.Logger,
) *IssuerResource {
	return &IssuerResource{
		client, scheme, pandaCluster, key, secretName, logger,
	}
}

// Ensure will manage cert-manager v1.Issuer for redpanda.vectorized.io custom resource
func (r *IssuerResource) Ensure(ctx context.Context) error {
	obj, err := r.obj()
	if err != nil {
		return fmt.Errorf("unable to construct object: %w", err)
	}

	_, err = resources.CreateIfNotExists(ctx, r, obj, r.logger)
	return err
}

// obj returns resource managed client.Object
func (r *IssuerResource) obj() (k8sclient.Object, error) {
	objLabels := labels.ForCluster(r.pandaCluster)
	objectMeta := metav1.ObjectMeta{
		Name:      r.Key().Name,
		Namespace: r.Key().Namespace,
		Labels:    objLabels,
	}

	var spec certmanagerv1.IssuerSpec
	if r.secretName == "" {
		spec = certmanagerv1.IssuerSpec{
			IssuerConfig: certmanagerv1.IssuerConfig{
				SelfSigned: &certmanagerv1.SelfSignedIssuer{},
			},
		}
	} else {
		spec = certmanagerv1.IssuerSpec{
			IssuerConfig: certmanagerv1.IssuerConfig{
				CA: &certmanagerv1.CAIssuer{
					SecretName: r.secretName,
				},
			},
		}
	}

	issuer := &certmanagerv1.Issuer{
		ObjectMeta: objectMeta,
		TypeMeta: metav1.TypeMeta{
			Kind:       "Issuer",
			APIVersion: "cert-manager.io/v1",
		},
		Spec: spec,
	}

	err := controllerutil.SetControllerReference(r.pandaCluster, issuer, r.scheme)
	if err != nil {
		return nil, err
	}

	return issuer, nil
}

// Key returns namespace/name object that is used to identify object.
// For reference please visit types.NamespacedName docs in k8s.io/apimachinery
func (r *IssuerResource) Key() types.NamespacedName {
	return r.key
}

// objRef returns the issuer's object reference
func (r *IssuerResource) objRef() *cmmetav1.ObjectReference {
	return &cmmetav1.ObjectReference{
		Name: r.Key().Name,
		Kind: issuerKind(),
	}
}

func issuerKind() string {
	var issuer certmanagerv1.Issuer
	return issuer.Kind
}
