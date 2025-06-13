package lifecycle

import (
	"context"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources"
)

// V1SimpleResourceRenderer is a simple resource renderer for v1alpha1 resources
type V1SimpleResourceRenderer struct {
	Client        client.Client
	Scheme        *runtime.Scheme
	ConfigFactory func(cluster *vectorizedv1alpha1.Cluster) resources.ConfigTemplater
	TLSSecretName string
	ClusterIssuer string
	SvcPorts      []resources.NamedServicePort
}

var _ SimpleResourceRenderer[vectorizedv1alpha1.Cluster, *vectorizedv1alpha1.Cluster] = (*V1SimpleResourceRenderer)(nil)

// Render renders all simple resources for a cluster
func (v V1SimpleResourceRenderer) Render(ctx context.Context, cluster *vectorizedv1alpha1.Cluster) ([]client.Object, error) {
	var objects []client.Object

	sa, err := resources.RenderServiceAccount(ctx, cluster)
	if err != nil {
		return nil, err
	}
	if sa != nil {
		objects = append(objects, sa)
	}

	cr, err := resources.RenderClusterRole(ctx, cluster)
	if err != nil {
		return nil, err
	}
	if cr != nil {
		objects = append(objects, cr)
	}

	crb, err := resources.RenderClusterRoleBinding(ctx, cluster)
	if err != nil {
		return nil, err
	}
	if crb != nil {
		objects = append(objects, crb)
	}

	pdb, err := resources.RenderPodDisruptionBudget(ctx, cluster)
	if err != nil {
		return nil, err
	}
	if pdb != nil {
		objects = append(objects, pdb)
	}

	if v.ConfigFactory != nil {
		cfg := v.ConfigFactory(cluster)
		cm, err := resources.RenderConfigMap(ctx, cluster, cfg)
		if err != nil {
			return nil, err
		}
		if cm != nil {
			objects = append(objects, cm)
		}
	}

	secret, err := resources.RenderLifecycleSecret(ctx, cluster, v.Client, v.Scheme)
	if err != nil {
		return nil, err
	}
	if secret != nil {
		objects = append(objects, secret)
	}

	if cluster.IsSASLOnInternalEnabled() && cluster.PandaproxyAPIInternal() != nil {
		su, err := resources.RenderSuperUsers(ctx, cluster, resources.ScramPandaproxyUsername, resources.PandaProxySuffix, v.Client, v.Scheme)
		if err != nil {
			return nil, err
		}
		if su != nil {
			objects = append(objects, su)
		}
	}

	if cluster.IsSASLOnInternalEnabled() && cluster.Spec.Configuration.SchemaRegistry != nil {
		su, err := resources.RenderSuperUsers(ctx, cluster, resources.ScramSchemaRegistryUsername, resources.SchemaRegistrySuffix, v.Client, v.Scheme)
		if err != nil {
			return nil, err
		}
		if su != nil {
			objects = append(objects, su)
		}
	}

	clusterSvc, err := resources.RenderClusterService(ctx, cluster, v.SvcPorts)
	if err != nil {
		return nil, err
	}
	if clusterSvc != nil {
		objects = append(objects, clusterSvc)
	}

	hs, err := resources.RenderHeadlessService(ctx, cluster)
	if err != nil {
		return nil, err
	}
	if hs != nil {
		objects = append(objects, hs)
	}

	lb, err := resources.RenderLoadBalancerService(ctx, cluster)
	if err != nil {
		return nil, err
	}
	if lb != nil {
		objects = append(objects, lb)
	}

	np, err := resources.RenderNodePortService(ctx, cluster)
	if err != nil {
		return nil, err
	}
	if np != nil {
		objects = append(objects, np)
	}

	pandaProxySubdomain := ""
	if cluster.FirstPandaproxyAPIExternal() != nil {
		pandaProxySubdomain = cluster.FirstPandaproxyAPIExternal().External.Subdomain
	}
	pandaProxyIngress, err := resources.RenderIngress(ctx, cluster, pandaProxySubdomain, v.TLSSecretName, v.ClusterIssuer)
	if err != nil {
		return nil, err
	}
	if pandaProxyIngress != nil {
		objects = append(objects, pandaProxyIngress)
	}

	return objects, nil
}

// WatchedResourceTypes returns the types of resources that should be watched
func (v V1SimpleResourceRenderer) WatchedResourceTypes() []client.Object {
	return []client.Object{
		&appsv1.Deployment{},
		&appsv1.StatefulSet{},
		&autoscalingv2.HorizontalPodAutoscaler{},
		&batchv1.Job{},
		&certmanagerv1.Certificate{},
		&certmanagerv1.Issuer{},
		&corev1.ConfigMap{},
		&corev1.Secret{},
		&corev1.ServiceAccount{},
		&corev1.Service{},
		&monitoringv1.PodMonitor{},
		&monitoringv1.ServiceMonitor{},
		&networkingv1.Ingress{},
		&policyv1.PodDisruptionBudget{},
		&rbacv1.ClusterRoleBinding{},
		&rbacv1.ClusterRole{},
		&rbacv1.RoleBinding{},
		&rbacv1.Role{},
	}
}