// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package resources

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/labels"
)

var _ Resource = &NodePortServiceResource{}

// NodePortServiceResource is part of the reconciliation of redpanda.vectorized.io CRD
// that assigns port on each node to enable external connectivity
type NodePortServiceResource struct {
	k8sclient.Client
	scheme       *runtime.Scheme
	pandaCluster *vectorizedv1alpha1.Cluster
	svcPorts     []NamedServiceNodePort
	logger       logr.Logger
}

// NewNodePortService creates NodePortServiceResource
func NewNodePortService(
	client k8sclient.Client,
	pandaCluster *vectorizedv1alpha1.Cluster,
	scheme *runtime.Scheme,
	svcPorts []NamedServiceNodePort,
	logger logr.Logger,
) *NodePortServiceResource {
	return &NodePortServiceResource{
		client,
		scheme,
		pandaCluster,
		svcPorts,
		logger.WithValues("ServiceType", "NodePort"),
	}
}

// Ensure will manage kubernetes v1.Service for redpanda.vectorized.io custom resource
func (r *NodePortServiceResource) Ensure(ctx context.Context) error {
	if r.pandaCluster.ExternalListener() == nil {
		return nil
	}

	obj, err := r.obj()
	if err != nil {
		return fmt.Errorf("unable to construct object: %w", err)
	}
	created, err := CreateIfNotExists(ctx, r, obj, r.logger)
	if err != nil || created {
		return err
	}
	var svc corev1.Service
	err = r.Get(ctx, r.Key(), &svc)
	if err != nil {
		return fmt.Errorf("error while fetching Service resource: %w", err)
	}

	copyPorts(obj.(*corev1.Service), &svc)
	_, err = Update(ctx, &svc, obj, r.Client, r.logger)
	return err
}

func copyPorts(newSvc, currentSvc *corev1.Service) {
	for i := range currentSvc.Spec.Ports {
		for j := range newSvc.Spec.Ports {
			if newSvc.Spec.Ports[j].Port == currentSvc.Spec.Ports[i].Port {
				newSvc.Spec.Ports[j].NodePort = currentSvc.Spec.Ports[i].NodePort
				break
			}
		}
	}
}

// obj returns resource managed client.Object
func (r *NodePortServiceResource) obj() (k8sclient.Object, error) {
	ports := make([]corev1.ServicePort, 0, len(r.svcPorts))
	for _, svcPort := range r.svcPorts {
		port := corev1.ServicePort{
			Name:       svcPort.Name,
			Protocol:   corev1.ProtocolTCP,
			Port:       int32(svcPort.Port),
			TargetPort: intstr.FromInt(svcPort.Port),
		}
		if !svcPort.GenerateNodePort {
			port.NodePort = int32(svcPort.Port)
		}
		ports = append(ports, port)
	}

	objLabels := labels.ForCluster(r.pandaCluster)
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: r.Key().Namespace,
			Name:      r.Key().Name,
			Labels:    objLabels,
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		Spec: corev1.ServiceSpec{
			// The service type node port assigned port to each node in the cluster.
			// This gives a way for operator to assign unused port to the redpanda cluster.
			// Reference:
			// https://kubernetes.io/docs/tutorials/services/source-ip/#source-ip-for-services-with-type-nodeport
			Type: corev1.ServiceTypeNodePort,
			// If you set service.spec.externalTrafficPolicy to the value Local,
			// kube-proxy only proxies proxy requests to local endpoints,
			// and does not forward traffic to other nodes.
			// Reference:
			// https://kubernetes.io/docs/tasks/access-application-cluster/create-external-load-balancer/#preserving-the-client-source-ip
			// https://blog.getambassador.io/externaltrafficpolicy-local-on-kubernetes-e66e498212f9
			ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeLocal,
			Ports:                 ports,
			// The selector is purposely set to nil. Our external connectivity doesn't use
			// kubernetes service as kafka protocol need to have access to each broker individually.
			Selector: nil,
		},
	}

	err := controllerutil.SetControllerReference(r.pandaCluster, svc, r.scheme)
	if err != nil {
		return nil, err
	}

	return svc, nil
}

// Key returns namespace/name object that is used to identify object.
// For reference please visit types.NamespacedName docs in k8s.io/apimachinery
func (r *NodePortServiceResource) Key() types.NamespacedName {
	return types.NamespacedName{Name: r.pandaCluster.Name + "-external", Namespace: r.pandaCluster.Namespace}
}

// RenderNodePortService renders a NodePort service
func RenderNodePortService(
	_ context.Context,
	cluster *vectorizedv1alpha1.Cluster,
) (k8sclient.Object, error) {
	svcPorts := collectNodePortsFromCluster(cluster)
	if len(svcPorts) == 0 {
		return nil, nil
	}

	var ports []corev1.ServicePort
	for _, svcPort := range svcPorts {
		port := corev1.ServicePort{
			Name:       svcPort.Name,
			Protocol:   corev1.ProtocolTCP,
			Port:       int32(svcPort.Port),
			TargetPort: intstr.FromInt32(int32(svcPort.Port)),
		}
		if !svcPort.GenerateNodePort {
			port.NodePort = int32(svcPort.Port)
		}
		ports = append(ports, port)
	}
	serviceName := cluster.Name + "-external"

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: cluster.Namespace,
			Name:      serviceName,
			Labels:    labels.ForCluster(cluster),
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeNodePort,
			ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeLocal,
			Ports: ports,
			Selector: nil,
		},
	}
	return svc, nil
}

// collectNodePortsFromCluster extracts NodePort configurations directly from cluster spec.
// This function replicates the exact behavior of the original networking.NewRedpandaPorts + collectNodePorts
// to avoid import cycles
func collectNodePortsFromCluster(cluster *vectorizedv1alpha1.Cluster) []NamedServiceNodePort {
	var nodeports []NamedServiceNodePort
	internalListener := cluster.InternalListener()
	adminAPIInternal := cluster.AdminAPIInternal()
	proxyAPIInternal := cluster.PandaproxyAPIInternal()
	for i, externalListener := range cluster.KafkaAPIExternalListeners() {
		if externalListener.External.ExcludeFromService {
			continue
		}

		portName := getPortName(externalListener.Name, ExternalListenerName, i)
		port := externalListener.Port
		externalPortIsGenerated := false

		if port == 0 {
			if internalListener != nil {
				port = internalListener.Port + 1
			}
			externalPortIsGenerated = true
		}

		nodeports = append(nodeports, NamedServiceNodePort{
			NamedServicePort: NamedServicePort{
				Name: portName,
				Port: port,
			},
			GenerateNodePort: externalPortIsGenerated,
		})
	}


	if adminAPIExternal := cluster.AdminAPIExternal(); adminAPIExternal != nil && !adminAPIExternal.External.ExcludeFromService {
		port := adminAPIExternal.Port
		externalPortIsGenerated := false

		if port == 0 {
			if adminAPIInternal != nil {
				port = adminAPIInternal.Port + 1
			}
			externalPortIsGenerated = true
		}

		nodeports = append(nodeports, NamedServiceNodePort{
			NamedServicePort: NamedServicePort{
				Name: AdminPortExternalName,
				Port: port,
			},
			GenerateNodePort: externalPortIsGenerated,
		})
	}

	for i, proxyExternal := range cluster.PandaproxyAPIExternalListeners() {
		if proxyExternal.External.ExcludeFromService {
			continue
		}

		portName := getPortName(proxyExternal.Name, PandaproxyPortExternalName, i)
		port := proxyExternal.Port
		externalPortIsGenerated := false

		if port == 0 {
			if proxyAPIInternal != nil {
				port = proxyAPIInternal.Port + 1
			}
			externalPortIsGenerated = true
		}

		nodeports = append(nodeports, NamedServiceNodePort{
			NamedServicePort: NamedServicePort{
				Name: portName,
				Port: port,
			},
			GenerateNodePort: externalPortIsGenerated,
		})
	}

	for i, sr := range cluster.SchemaRegistryListeners() {
		if !sr.IsExternallyAvailable() || sr.External.ExcludeFromService {
			continue
		}

		portName := getPortName(sr.Name, SchemaRegistryPortName, i)
		externalPortIsGenerated := !sr.External.StaticNodePort

		nodeports = append(nodeports, NamedServiceNodePort{
			NamedServicePort: NamedServicePort{
				Name: portName,
				Port: sr.Port,
			},
			GenerateNodePort: externalPortIsGenerated,
		})
	}
	return nodeports
}

func getPortName(listenerName, baseName string, i int) string {
	if listenerName != "" {
		return listenerName
	}
	if i == 0 {
		return baseName
	}
	return fmt.Sprintf("%s-%d", baseName, i)
}