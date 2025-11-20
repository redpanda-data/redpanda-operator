// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package multicluster

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"hash/fnv"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/andrewstucki/locking/raft"
	transportv1 "github.com/andrewstucki/locking/raft/proto/gen/transport/v1"
	"github.com/spf13/cobra"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type CheckOptions struct {
	OperatorNamespace string
	ServiceName       string
	ContextName       string

	serverOverride string
}

func (o *CheckOptions) BindFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&o.OperatorNamespace, "operator-namespace", "redpanda-system", "namespace in which the operator will be deployed")
	cmd.Flags().StringVar(&o.ServiceName, "operator-name", "redpanda-multicluster-operator", "name of the operator being deployed")
	cmd.Flags().StringVar(&o.ContextName, "context", "", "cluster context")
}

func (o *CheckOptions) Validate() error {
	if o.ContextName == "" {
		return errors.New("must specify context")
	}

	if strings.Contains(o.ContextName, "://") {
		parsed, err := url.Parse(o.ContextName)
		if err != nil {
			return errors.New("context must be of either the form \"context-name\" or \"context-name://server-address\"")
		}
		if parsed.Host == "" || parsed.Scheme == "" {
			return errors.New("context must be of either the form \"context-name\" or \"context-name://server-address\"")
		}
		o.ContextName = parsed.Scheme
		o.serverOverride = parsed.Host
	}

	return nil
}

func CheckCommand(multiclusterOptions *MulticlusterOptions) *cobra.Command {
	var options CheckOptions

	cmd := &cobra.Command{
		Use: "check",
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunCheck(cmd.Context(), &options, multiclusterOptions)
		},
	}

	options.BindFlags(cmd)

	return cmd
}

func RunCheck(
	ctx context.Context,
	opts *CheckOptions,
	multiclusterOpts *MulticlusterOptions,
) error {
	if err := multiclusterOpts.Validate(); err != nil {
		return err
	}

	if err := opts.Validate(); err != nil {
		return err
	}

	config, err := configFromContext(opts.ContextName)
	if err != nil {
		return err
	}

	cl, err := client.New(config, client.Options{})
	if err != nil {
		return err
	}

	// deployment check

	var deployment appsv1.Deployment
	if err := cl.Get(ctx, types.NamespacedName{
		Namespace: opts.OperatorNamespace,
		Name:      opts.ServiceName,
	}, &deployment); err != nil {
		return err
	}

	nodeName := deployment.Annotations["cluster.redpanda.com/multicluster-node-id"]
	h := fnv.New64a()
	_, _ = h.Write([]byte(nodeName))
	nodeID := h.Sum64()

	// service check

	var service corev1.Service
	if err := cl.Get(ctx, types.NamespacedName{
		Namespace: opts.OperatorNamespace,
		Name:      opts.ServiceName,
	}, &service); err != nil {
		return err
	}

	insecure := opts.serverOverride != ""
	address := opts.serverOverride
	if address == "" {
		if len(service.Status.LoadBalancer.Ingress) == 0 {
			return errors.New("could not determine external ip")
		}
		ingress := service.Status.LoadBalancer.Ingress[0]
		if ingress.IP != "" {
			address = ingress.IP + ":9443"
		} else {
			address = ingress.Hostname + ":9443"
		}
		if address == "" {
			return errors.New("could not determine external ip")
		}
	}

	var certificates corev1.Secret
	if err := cl.Get(ctx, types.NamespacedName{
		Namespace: opts.OperatorNamespace,
		Name:      opts.ServiceName + "-certificates",
	}, &certificates); err != nil {
		return err
	}

	// TODO: acls check

	// cert checks

	if certificates.Data == nil {
		return errors.New("certificate does not contain parseable data")
	}

	ca, ok := certificates.Data["ca.crt"]
	if !ok {
		return errors.New("certificate does not contain ca")
	}

	cert, ok := certificates.Data["tls.crt"]
	if !ok {
		return errors.New("certificate does not contain certificate")
	}

	pk, ok := certificates.Data["tls.key"]
	if !ok {
		return errors.New("certificate does not contain private key")
	}

	certificate, err := tls.X509KeyPair(cert, pk)
	if err != nil {
		return fmt.Errorf("certificate is not parseable: %v", err)
	}

	if certificate.Leaf == nil {
		return errors.New("certificate does not contain a leaf certificate")
	}

	serverAuth := false
	clientAuth := false
	for _, usage := range certificate.Leaf.ExtKeyUsage {
		if x509.ExtKeyUsageServerAuth == usage {
			serverAuth = true
		}
		if x509.ExtKeyUsageClientAuth == usage {
			clientAuth = true
		}
	}

	names := map[string]struct{}{}

	names[certificate.Leaf.Subject.CommonName] = struct{}{}
	for _, name := range certificate.Leaf.DNSNames {
		names[name] = struct{}{}
	}

	for _, ip := range certificate.Leaf.IPAddresses {
		names[ip.String()] = struct{}{}
	}

	sortedNames := []string{}
	for name := range names {
		sortedNames = append(sortedNames, name)
	}
	sort.Strings(sortedNames)

	fmt.Printf("Server auth TLS certificate: %v\n", serverAuth)
	fmt.Printf("Client auth TLS certificate: %v\n", clientAuth)
	fmt.Printf("Names: [%s]\n", strings.Join(sortedNames, ", "))

	// raft checks

	var raftClient transportv1.TransportServiceClient
	if insecure {
		raftClient, err = raft.InsecureClientFor(raft.LockConfiguration{
			CA:          ca,
			Certificate: cert,
			PrivateKey:  pk,
		}, raft.LockerNode{
			ID:      nodeID,
			Address: address,
		})
	} else {
		raftClient, err = raft.ClientFor(raft.LockConfiguration{
			CA:          ca,
			Certificate: cert,
			PrivateKey:  pk,
		}, raft.LockerNode{
			ID:      nodeID,
			Address: address,
		})
	}
	if err != nil {
		return err
	}

	response, err := raftClient.Check(ctx, &transportv1.CheckRequest{})
	if err != nil {
		return err
	}

	unhealthyNodes := []string{}
	for _, node := range response.UnhealthyNodes {
		unhealthyNodes = append(unhealthyNodes, strconv.Itoa(int(node)))
	}

	fmt.Printf("Has leader: %v\n", response.HasLeader)
	fmt.Printf("Unhealthy nodes: [%s]\n", strings.Join(unhealthyNodes, ", "))
	fmt.Printf("Leader Meta: %s\n", string(response.Meta))
	return nil
}

func configFromContext(contextName string) (*rest.Config, error) {
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{CurrentContext: contextName},
	).ClientConfig()
}
