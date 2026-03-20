// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package redpanda

import (
	"context"
	"fmt"

	"github.com/redpanda-data/common-go/otelutil/log"
	corev1 "k8s.io/api/core/v1"
	k8sapierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	rendermulticluster "github.com/redpanda-data/redpanda-operator/operator/multicluster"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster/bootstrap"
)

const (
	// caOrganization is the organization name embedded in generated CA certificates.
	caOrganization = "Redpanda"
)

//+kubebuilder:rbac:groups=cluster.redpanda.com,resources=stretchclusters,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch

// CAReconciler watches StretchCluster resources and ensures that a shared root
// CA Secret exists for each operator-managed TLS cert name. The CA is created
// on the local (leader) cluster and then synced to every known remote cluster.
type CAReconciler struct {
	manager multicluster.Manager
}

func (r *CAReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	logger := ctrllog.FromContext(ctx).WithValues("controller", "ca", "cluster", req.ClusterName, "namespace", req.Namespace, "name", req.Name)

	cl, err := r.manager.GetCluster(ctx, req.ClusterName)
	if err != nil {
		return ctrl.Result{}, nil
	}
	k8sClient := cl.GetClient()

	var stretchCluster redpandav1alpha2.StretchCluster
	if err := k8sClient.Get(ctx, req.NamespacedName, &stretchCluster); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Determine which cert names require operator-managed CAs.
	managedCerts := rendermulticluster.BootstrappedCertNames(&stretchCluster.Spec)
	if len(managedCerts) == 0 {
		logger.V(log.DebugLevel).Info("no operator-managed TLS certs, skipping CA reconciliation")
		return ctrl.Result{}, nil
	}

	// For each managed cert, ensure the CA Secret exists locally and is
	// synced to all clusters.
	for _, certName := range managedCerts {
		secretName := rendermulticluster.CASecretName(stretchCluster.Name, certName)

		// Ensure the CA Secret exists on the local cluster.
		caSecret, err := r.ensureLocalCA(ctx, k8sClient, secretName, stretchCluster.Namespace)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("ensuring local CA for cert %q: %w", certName, err)
		}

		// Sync the CA Secret to all known clusters.
		if err := r.syncCAToAllClusters(ctx, caSecret, stretchCluster.Namespace); err != nil {
			return ctrl.Result{}, fmt.Errorf("syncing CA for cert %q: %w", certName, err)
		}

		logger.V(log.InfoLevel).Info("CA secret synced", "cert", certName, "secret", secretName)
	}

	return ctrl.Result{}, nil
}

// ensureLocalCA ensures a CA Secret exists on the local cluster. It attempts a
// one-shot Create; if the Secret already exists the existing one is returned
// unchanged. This avoids a cache-read race where a stale informer cache could
// cause us to regenerate and overwrite a CA that was already created and synced
// to remote clusters.
func (r *CAReconciler) ensureLocalCA(
	ctx context.Context,
	k8sClient client.Client,
	secretName, namespace string,
) (*corev1.Secret, error) {
	// Generate a new root CA optimistically.
	ca, err := bootstrap.GenerateCA(caOrganization, secretName, nil)
	if err != nil {
		return nil, fmt.Errorf("generating CA: %w", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSCertKey:       ca.Bytes(),
			corev1.TLSPrivateKeyKey: ca.PrivateKeyBytes(),
			// ca.crt is the conventional key cert-manager uses for the CA public cert.
			"ca.crt": ca.Bytes(),
		},
	}

	// Try to create — if it already exists, just fetch and return the existing one.
	if err := k8sClient.Create(ctx, secret); err != nil {
		if !k8sapierrors.IsAlreadyExists(err) {
			return nil, fmt.Errorf("creating CA secret %s/%s: %w", namespace, secretName, err)
		}

		var existing corev1.Secret
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: secretName, Namespace: namespace}, &existing); err != nil {
			return nil, fmt.Errorf("getting existing CA secret %s/%s: %w", namespace, secretName, err)
		}
		return &existing, nil
	}

	return secret, nil
}

// syncCAToAllClusters copies the CA Secret to every cluster known to the
// manager. The Secret is created or updated idempotently on each cluster.
func (r *CAReconciler) syncCAToAllClusters(
	ctx context.Context,
	caSecret *corev1.Secret,
	namespace string,
) error {
	for _, clusterName := range r.manager.GetClusterNames() {
		cl, err := r.manager.GetCluster(ctx, clusterName)
		if err != nil {
			return fmt.Errorf("getting cluster %q: %w", clusterName, err)
		}
		remoteClient := cl.GetClient()

		remote := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      caSecret.Name,
				Namespace: namespace,
			},
		}

		_, err = controllerutil.CreateOrUpdate(ctx, remoteClient, remote, func() error {
			remote.Type = corev1.SecretTypeTLS
			remote.Data = caSecret.Data
			return nil
		})
		if err != nil {
			return fmt.Errorf("syncing CA secret to cluster %q: %w", clusterName, err)
		}
	}

	return nil
}

// SetupCAController registers the CAReconciler with the multicluster manager.
func SetupCAController(ctx context.Context, mgr multicluster.Manager) error {
	return mcbuilder.ControllerManagedBy(mgr).WithOptions(
		controller.TypedOptions[mcreconcile.Request]{
			SkipNameValidation: ptr.To(true),
		}).
		For(&redpandav1alpha2.StretchCluster{},
			mcbuilder.WithEngageWithLocalCluster(true),
			mcbuilder.WithEngageWithProviderClusters(true)).
		Complete(&CAReconciler{manager: mgr})
}
