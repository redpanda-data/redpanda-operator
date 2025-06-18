package lifecycle

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	v1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources"
)

// V1SimpleResourceRenderer is a simple resource renderer for v1alpha1 resources
type V1SimpleResourceRenderer struct {
	Client        client.Client
	TLSSecretName string
	ClusterIssuer string
}

var _ SimpleResourceRenderer[vectorizedv1alpha1.Cluster, *vectorizedv1alpha1.Cluster] = (*V1SimpleResourceRenderer)(nil)

func NewV1SimpleResourceRenderer(mgr ctrl.Manager, cloudSecrets CloudSecretsFlags) *V1SimpleResourceRenderer {
	return &V1SimpleResourceRenderer{
		Client: mgr.GetClient(),
	}
}

// Render renders all simple resources for a cluster
func (v V1SimpleResourceRenderer) Render(ctx context.Context, cluster *vectorizedv1alpha1.Cluster) ([]client.Object, error) {
	var objects []client.Object

	if o := resources.RenderClusterRole(); o != nil {
		objects = append(objects, o)
	}
	if o := resources.RenderClusterRoleBinding(cluster); o != nil {
		objects = append(objects, o)
	}
	if o := resources.RenderServiceAccount(cluster); o != nil {
		objects = append(objects, o)
	}

	if svc := resources.RenderClusterService(cluster); svc != nil {
		for _, s := range svc {
			objects = append(objects, s)
		}
	}
	if o := resources.RenderHeadlessService(cluster); o != nil {
		objects = append(objects, o)
	}
	//if o := resources.RenderNodePortService(cluster); o != nil {
	//	objects = append(objects, o)
	//}
	if o := resources.RenderLoadBalancerService(cluster); o != nil {
		objects = append(objects, o)
	}

	//if cm, err := resources.RenderConfigMap(cluster, v.ConfigFactory(cluster)); err != nil {
	//	return nil, err
	//} else if cm != nil {
	//	objects = append(objects, cm)
	//}
	//
	if o := resources.RenderPodDisruptionBudget(cluster); o != nil {
		objects = append(objects, o)
	}
	//
	sec, err := resources.RenderSuperUsers(ctx, cluster, v.Client)
	if err != nil {
		return nil, err
	}
	if sec != nil {
		for _, s := range sec {
			objects = append(objects, s)
		}
	}
	//
	if o, err := resources.RenderLifecycleSecret(ctx, cluster, v.Client); err != nil {
		return nil, err
	} else if o != nil {
		objects = append(objects, o)
	}
	//
	if ing, err := resources.RenderIngress(cluster, v.TLSSecretName, v.ClusterIssuer); err != nil {
		return nil, err
	} else if ing != nil {
		objects = append(objects, ing)
	}

	return objects, nil
}

// WatchedResourceTypes returns the types of resources that should be watched
func (v V1SimpleResourceRenderer) WatchedResourceTypes() []client.Object {
	return []client.Object{
		&rbacv1.ClusterRole{},
		&rbacv1.ClusterRoleBinding{},
		&corev1.ServiceAccount{},
		&corev1.Service{},
		&v1.PodDisruptionBudget{},
		&corev1.Secret{},
		&networkingv1.Ingress{},
	}
}
