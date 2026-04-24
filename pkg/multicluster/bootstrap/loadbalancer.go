// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	// PeerLoadBalancerPort is the port peers dial on another cluster's
	// operator for raft gRPC.
	PeerLoadBalancerPort = 9443

	// peerLoadBalancerSuffix is the suffix appended to the operator's helm
	// fullname to form the bootstrap-managed peer Service name. Kept
	// distinct from the helm chart's own Service (which uses the
	// fullname unsuffixed) so the two don't collide when
	// multicluster.service.enabled is flipped on/off.
	peerLoadBalancerSuffix = "-multicluster-peer"

	// defaultLoadBalancerProvisionTimeout is how long we wait for the
	// cloud provider to assign an external IP/hostname to a newly
	// created LoadBalancer Service before giving up.
	defaultLoadBalancerProvisionTimeout = 10 * time.Minute

	// defaultLoadBalancerPollInterval is how frequently we re-read the
	// Service to check for a provisioned ingress.
	defaultLoadBalancerPollInterval = 5 * time.Second
)

// LoadBalancerLabelKey marks Services created by the bootstrap so callers
// can identify them without inferring from the name.
const LoadBalancerLabelKey = "operator.redpanda.com/bootstrap-managed"

// PeerLoadBalancerConfig tunes EnsurePeerLoadBalancer. Zero values use the
// defaults above.
type PeerLoadBalancerConfig struct {
	// ProvisionTimeout bounds how long to wait for the cloud provider
	// to assign an ingress address.
	ProvisionTimeout time.Duration

	// PollInterval controls how often the Service is re-read while
	// waiting for provisioning.
	PollInterval time.Duration

	// Annotations is merged onto the Service at creation time (e.g.
	// "service.beta.kubernetes.io/aws-load-balancer-type: nlb" for
	// EKS NLB, or an internal-LB annotation for cloud-native private
	// peering). Ignored when a Service already exists — we don't
	// reshape an already-provisioned LB.
	Annotations map[string]string
}

// peerLoadBalancerName returns the Service name the bootstrap uses for a
// cluster's peer-facing LoadBalancer. Prefers the per-cluster helm fullname
// (cluster.Name) and falls back to the shared ServiceName so the result is
// stable regardless of whether the caller passes a per-cluster override.
func peerLoadBalancerName(cluster RemoteConfiguration, config BootstrapClusterConfiguration) string {
	base := cluster.Name
	if base == "" {
		base = config.ServiceName
	}
	return base + peerLoadBalancerSuffix
}

// EnsurePeerLoadBalancer creates (or reuses) a LoadBalancer Service that
// fronts the operator pod on a single remote cluster and waits for the
// cloud provider to assign an external address. The returned address is a
// hostname (AWS ELB) or an IP literal (Azure/GCP) — whichever the provider
// publishes. Callers feed this back into the operator's --peer list so
// peers dial a stable endpoint.
//
// The Service is labelled with LoadBalancerLabelKey so it's distinguishable
// from the helm-managed operator Service. The selector matches the
// operator pods that a helm install with release name == cluster.Name will
// later create, so the LB is provisioned before the pods exist and starts
// forwarding as soon as they come up.
//
// Idempotent — repeated calls with the same configuration reuse the
// existing Service and re-read the already-provisioned address.
func EnsurePeerLoadBalancer(
	ctx context.Context,
	cluster RemoteConfiguration,
	config BootstrapClusterConfiguration,
	lbConfig PeerLoadBalancerConfig,
) (string, error) {
	restConfig, err := cluster.Config()
	if err != nil {
		return "", fmt.Errorf("getting REST config for %s: %w", cluster.ContextName, err)
	}

	cl, err := client.New(restConfig, client.Options{})
	if err != nil {
		return "", fmt.Errorf("initializing client for %s: %w", cluster.ContextName, err)
	}

	if config.EnsureNamespace {
		if err := EnsureNamespace(ctx, config.OperatorNamespace, cl); err != nil {
			return "", fmt.Errorf("ensuring namespace exists on %s: %w", cluster.ContextName, err)
		}
	}

	instance := cluster.Name
	if instance == "" {
		instance = config.ServiceName
	}

	name := peerLoadBalancerName(cluster, config)
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: config.OperatorNamespace,
		},
	}

	if _, err := controllerutil.CreateOrUpdate(ctx, cl, svc, func() error {
		// Labels are refreshed on every call so we don't orphan
		// Services that were created before the label was introduced.
		if svc.Labels == nil {
			svc.Labels = map[string]string{}
		}
		svc.Labels[LoadBalancerLabelKey] = "true"
		svc.Labels["app.kubernetes.io/name"] = config.ServiceName
		svc.Labels["app.kubernetes.io/instance"] = instance

		// Only seed annotations on create — once the LB exists, the
		// cloud provider may have rewritten annotations (e.g. AWS
		// stamps the ELB ID), and we don't want to clobber those on
		// a re-run.
		if len(svc.Annotations) == 0 && len(lbConfig.Annotations) > 0 {
			svc.Annotations = map[string]string{}
			for k, v := range lbConfig.Annotations {
				svc.Annotations[k] = v
			}
		}

		svc.Spec.Type = corev1.ServiceTypeLoadBalancer
		svc.Spec.Selector = map[string]string{
			"app.kubernetes.io/name":     config.ServiceName,
			"app.kubernetes.io/instance": instance,
		}
		// Peer dials happen over TLS before pods report Ready (health
		// checks run on the same port), so publish not-ready
		// addresses: the LB becomes addressable as soon as the pod
		// attaches rather than after readiness.
		svc.Spec.PublishNotReadyAddresses = true

		// Reconcile the raft port. Preserve NodePort when one is
		// already assigned so we don't cause the cloud provider to
		// re-provision the LB on every run.
		var nodePort int32
		for _, p := range svc.Spec.Ports {
			if p.Name == "raft" {
				nodePort = p.NodePort
				break
			}
		}
		svc.Spec.Ports = []corev1.ServicePort{{
			Name:       "raft",
			Protocol:   corev1.ProtocolTCP,
			Port:       PeerLoadBalancerPort,
			TargetPort: intstr.FromInt(PeerLoadBalancerPort),
			NodePort:   nodePort,
		}}
		return nil
	}); err != nil {
		return "", fmt.Errorf("creating/updating Service %s/%s on %s: %w",
			config.OperatorNamespace, name, cluster.ContextName, err)
	}

	provisionTimeout := lbConfig.ProvisionTimeout
	if provisionTimeout == 0 {
		provisionTimeout = defaultLoadBalancerProvisionTimeout
	}
	pollInterval := lbConfig.PollInterval
	if pollInterval == 0 {
		pollInterval = defaultLoadBalancerPollInterval
	}

	var address string
	err = wait.PollUntilContextTimeout(ctx, pollInterval, provisionTimeout, true, func(ctx context.Context) (bool, error) {
		var current corev1.Service
		if err := cl.Get(ctx, client.ObjectKeyFromObject(svc), &current); err != nil {
			return false, fmt.Errorf("reading Service %s/%s: %w",
				config.OperatorNamespace, name, err)
		}
		for _, ing := range current.Status.LoadBalancer.Ingress {
			if ing.Hostname != "" {
				address = ing.Hostname
				return true, nil
			}
			if ing.IP != "" {
				address = ing.IP
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		return "", fmt.Errorf("waiting for LoadBalancer ingress on %s (Service %s/%s): %w",
			cluster.ContextName, config.OperatorNamespace, name, err)
	}
	if address == "" {
		return "", errors.New("LoadBalancer reported ready but published no address")
	}
	return address, nil
}
