// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package vectorized

import (
	"context"
	"fmt"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/clusterconfiguration"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/networking"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/nodepools"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources/certmanager"
)

type attachedResources struct {
	ctx            context.Context
	reconciler     *ClusterReconciler
	log            logr.Logger
	cluster        *vectorizedv1alpha1.Cluster
	items          map[string]resources.Resource
	order          []string
	autoDeletePVCs bool
}

const (
	bootstrapService        = "BootstrapService"
	clusterRole             = "ClusterRole"
	clusterRoleBinding      = "ClusterRoleBinding"
	clusterService          = "ClusterPorts"
	configMap               = "ConfigMap"
	headlessService         = "HeadlessService"
	ingress                 = "Ingress"
	nodeportService         = "NodeportService"
	pki                     = "PKI"
	podDisruptionBudget     = "PodDisruptionBudget"
	proxySuperuser          = "ProxySuperuser"
	schemaRegistrySuperUser = "SchemaRegistrySuperUser"
	rpkSuperUser            = "RpkSuperUser"
	serviceAccount          = "ServiceAccount"
	secret                  = "Secret"
	statefulSet             = "StatefulSet"
	nodePool                = "NodePool"
)

func newAttachedResources(ctx context.Context, r *ClusterReconciler, log logr.Logger, cluster *vectorizedv1alpha1.Cluster) *attachedResources {
	return &attachedResources{
		ctx:            ctx,
		reconciler:     r,
		log:            log,
		cluster:        cluster,
		items:          map[string]resources.Resource{},
		autoDeletePVCs: r.AutoDeletePVCs,
	}
}

type resourceKey string

func (a *attachedResources) Ensure() (ctrl.Result, error) {
	result := ctrl.Result{}
	var errs error

	for _, key := range a.order {
		resource, ok := a.items[key]
		if !ok {
			continue
		}
		if resource == nil {
			continue
		}

		err := resource.Ensure(context.WithValue(a.ctx, resourceKey("resource"), key))
		var e *resources.RequeueAfterError
		if errors.As(err, &e) {
			a.log.Info(e.Error())
			if result.RequeueAfter < e.RequeueAfter {
				result = ctrl.Result{RequeueAfter: e.RequeueAfter}
			}
		} else if err != nil {
			a.log.Error(err, "Failed to reconcile resource", "resource", key)
			errs = errors.Join(errs, err)
		}
	}
	// Controller-runtime does not allow returning a result and an error at the same time.
	// We choose to prioritize the error - if there is an error, this is more important
	// to us to return than the reconcile.
	// Downstream functions are expected to only return errors, if there is an
	// actual error condition, not generally to cause a requeue (must use result for this purpose).
	if errs != nil {
		return ctrl.Result{}, errs
	}
	return result, nil
}

func (a *attachedResources) bootstrapService() {
	// if already initialized, exit immediately
	if _, ok := a.items[bootstrapService]; ok {
		return
	}
	redpandaPorts := networking.NewRedpandaPorts(a.cluster)
	loadbalancerPorts := collectLBPorts(redpandaPorts)
	a.items[bootstrapService] = resources.NewLoadBalancerService(a.reconciler.Client, a.cluster, a.reconciler.Scheme, loadbalancerPorts, true, a.log)
	a.order = append(a.order, bootstrapService)
}

func (a *attachedResources) getBootstrapService() *resources.LoadBalancerServiceResource {
	a.bootstrapService()
	return a.items[bootstrapService].(*resources.LoadBalancerServiceResource)
}

func (a *attachedResources) getBootstrapServiceKey() types.NamespacedName {
	return a.getBootstrapService().Key()
}

func (a *attachedResources) clusterRole() {
	// if already initialized, exit immediately
	if _, ok := a.items[clusterRole]; ok {
		return
	}
	a.items[clusterRole] = resources.NewClusterRole(a.reconciler.Client, a.cluster, a.reconciler.Scheme, a.log)
	a.order = append(a.order, clusterRole)
}

func (a *attachedResources) clusterRoleBinding() {
	// if already initialized, exit immediately
	if _, ok := a.items[clusterRoleBinding]; ok {
		return
	}
	a.items[clusterRoleBinding] = resources.NewClusterRoleBinding(a.reconciler.Client, a.cluster, a.reconciler.Scheme, a.log)
	a.order = append(a.order, clusterRoleBinding)
}

func (a *attachedResources) getClusterRoleBinding() *resources.ClusterRoleBindingResource {
	a.clusterRoleBinding()
	return a.items[clusterRoleBinding].(*resources.ClusterRoleBindingResource)
}

func (a *attachedResources) clusterService() {
	// if already initialized, exit immediately
	if _, ok := a.items[clusterService]; ok {
		return
	}
	redpandaPorts := networking.NewRedpandaPorts(a.cluster)
	clusterPorts := collectClusterPorts(redpandaPorts, a.cluster)
	a.items[clusterService] = resources.NewClusterService(a.reconciler.Client, a.cluster, a.reconciler.Scheme, clusterPorts, a.log)
	a.order = append(a.order, clusterService)
}

func (a *attachedResources) getClusterService() *resources.ClusterServiceResource {
	a.clusterService()
	return a.items[clusterService].(*resources.ClusterServiceResource)
}

func (a *attachedResources) getClusterServiceName() string {
	return a.getClusterService().Key().Name
}

func (a *attachedResources) getClusterServiceFQDN() string {
	return a.getClusterService().ServiceFQDN(a.reconciler.clusterDomain)
}

func (a *attachedResources) configMap(cfg *clusterconfiguration.CombinedCfg) {
	// if already initialized, exit immediately
	if _, ok := a.items[configMap]; ok {
		return
	}

	a.items[configMap] = resources.NewConfigMap(a.reconciler.Client, a.cluster, a.reconciler.Scheme, cfg, a.log)
	a.order = append(a.order, configMap)
}

func (a *attachedResources) getConfigMap(cfg *clusterconfiguration.CombinedCfg) *resources.ConfigMapResource {
	a.configMap(cfg)
	return a.items[configMap].(*resources.ConfigMapResource)
}

func (a *attachedResources) headlessService() {
	// if already initialized, exit immediately
	if _, ok := a.items[headlessService]; ok {
		return
	}
	redpandaPorts := networking.NewRedpandaPorts(a.cluster)
	headlessPorts := collectHeadlessPorts(redpandaPorts)

	a.items[headlessService] = resources.NewHeadlessService(a.reconciler.Client, a.cluster, a.reconciler.Scheme, headlessPorts, a.log)
	a.order = append(a.order, headlessService)
}

func (a *attachedResources) getHeadlessService() *resources.HeadlessServiceResource {
	a.headlessService()
	return a.items[headlessService].(*resources.HeadlessServiceResource)
}

func (a *attachedResources) getHeadlessServiceKey() types.NamespacedName {
	return a.getHeadlessService().Key()
}

func (a *attachedResources) getHeadlessServiceName() string {
	return a.getHeadlessServiceKey().Name
}

func (a *attachedResources) getHeadlessServiceFQDN() string {
	return a.getHeadlessService().HeadlessServiceFQDN(a.reconciler.clusterDomain)
}

func (a *attachedResources) ingress() {
	// if already initialized, exit immediately
	if _, ok := a.items[ingress]; ok {
		return
	}
	clusterServiceName := a.getClusterServiceName()

	var pandaProxyIngressConfig *vectorizedv1alpha1.IngressConfig
	subdomain := ""
	proxyAPIExternal := a.cluster.PandaproxyAPIExternal()
	if proxyAPIExternal != nil {
		subdomain = proxyAPIExternal.External.Subdomain
		pandaProxyIngressConfig = proxyAPIExternal.External.Ingress
	}

	a.items[ingress] = resources.NewIngress(
		a.reconciler.Client,
		a.cluster,
		a.reconciler.Scheme,
		subdomain,
		clusterServiceName,
		resources.PandaproxyPortExternalName,
		a.log).WithAnnotations(map[string]string{resources.SSLPassthroughAnnotation: "true"}).WithUserConfig(pandaProxyIngressConfig)
	a.order = append(a.order, ingress)
}

func (a *attachedResources) nodeportService() {
	// if already initialized, exit immediately
	if _, ok := a.items[nodeportService]; ok {
		return
	}
	redpandaPorts := networking.NewRedpandaPorts(a.cluster)
	nodeports := collectNodePorts(redpandaPorts)
	a.items[nodeportService] = resources.NewNodePortService(a.reconciler.Client, a.cluster, a.reconciler.Scheme, nodeports, a.log)
	a.order = append(a.order, nodeportService)
}

func (a *attachedResources) getNodeportService() *resources.NodePortServiceResource {
	a.nodeportService()
	return a.items[nodeportService].(*resources.NodePortServiceResource)
}

func (a *attachedResources) getNodeportServiceKey() types.NamespacedName {
	return a.getNodeportService().Key()
}

func (a *attachedResources) pki() error {
	// if already initialized, exit immediately
	if _, ok := a.items[pki]; ok {
		return nil
	}

	newPKI, err := certmanager.NewPki(a.ctx, a.reconciler.Client, a.cluster, a.getHeadlessServiceFQDN(), a.getClusterServiceFQDN(), a.reconciler.Scheme, a.log)
	if err != nil {
		return fmt.Errorf("creating pki: %w", err)
	}

	a.items[pki] = newPKI
	a.order = append(a.order, pki)
	return nil
}

func (a *attachedResources) getPKI() (*certmanager.PkiReconciler, error) {
	err := a.pki()
	if err != nil {
		return nil, err
	}
	return a.items[pki].(*certmanager.PkiReconciler), nil
}

func (a *attachedResources) podDisruptionBudget() {
	// if already initialized, exit immediately
	if _, ok := a.items[podDisruptionBudget]; ok {
		return
	}
	a.items[podDisruptionBudget] = resources.NewPDB(a.reconciler.Client, a.cluster, a.reconciler.Scheme, a.log)
	a.order = append(a.order, podDisruptionBudget)
}

func (a *attachedResources) proxySuperuser() {
	// if already initialized, exit immediately
	if _, ok := a.items[proxySuperuser]; ok {
		return
	}

	var proxySASLUser *resources.SuperUsersResource
	a.items[proxySuperuser] = proxySASLUser
	if a.cluster.IsSASLOnInternalEnabled() && a.cluster.PandaproxyAPIInternal() != nil {
		a.items[proxySuperuser] = resources.NewSuperUsers(a.reconciler.Client, a.cluster, a.reconciler.Scheme, resources.ScramPandaproxyUsername, resources.PandaProxySuffix, a.log)
	}
	a.order = append(a.order, proxySuperuser)
}

func (a *attachedResources) getProxySuperuser() *resources.SuperUsersResource {
	a.proxySuperuser()
	return a.items[proxySuperuser].(*resources.SuperUsersResource)
}

func (a *attachedResources) getProxySuperUserKey() types.NamespacedName {
	if a.getProxySuperuser() == nil {
		return types.NamespacedName{}
	}
	return a.getProxySuperuser().Key()
}

func (a *attachedResources) schemaRegistrySuperUser() {
	// if already initialized, exit immediately
	if _, ok := a.items[schemaRegistrySuperUser]; ok {
		return
	}

	var schemaRegistrySASLUser *resources.SuperUsersResource
	a.items[schemaRegistrySuperUser] = schemaRegistrySASLUser
	if a.cluster.IsSASLOnInternalEnabled() && a.cluster.Spec.Configuration.SchemaRegistry != nil {
		a.items[schemaRegistrySuperUser] = resources.NewSuperUsers(a.reconciler.Client, a.cluster, a.reconciler.Scheme, resources.ScramSchemaRegistryUsername, resources.SchemaRegistrySuffix, a.log)
	}
	a.order = append(a.order, schemaRegistrySuperUser)
}

func (a *attachedResources) getSchemaRegistrySuperUser() *resources.SuperUsersResource {
	a.schemaRegistrySuperUser()
	return a.items[schemaRegistrySuperUser].(*resources.SuperUsersResource)
}

func (a *attachedResources) getSchemaRegistrySuperUserKey() types.NamespacedName {
	if a.getSchemaRegistrySuperUser() == nil {
		return types.NamespacedName{}
	}
	return a.getSchemaRegistrySuperUser().Key()
}

func (a *attachedResources) rpkSuperUser() {
	// if already initialized, exit immediately
	if _, ok := a.items[rpkSuperUser]; ok {
		return
	}

	var rpkSASLUser *resources.SuperUsersResource
	a.items[rpkSuperUser] = rpkSASLUser
	if a.cluster.IsSASLOnInternalEnabled() {
		a.items[rpkSuperUser] = resources.NewSuperUsers(a.reconciler.Client, a.cluster, a.reconciler.Scheme, resources.ScramRPKUsername, resources.RPKSuffix, a.log)
	}
	a.order = append(a.order, rpkSuperUser)
}

func (a *attachedResources) getRPKSuperUser() *resources.SuperUsersResource {
	a.rpkSuperUser()
	return a.items[rpkSuperUser].(*resources.SuperUsersResource)
}

func (a *attachedResources) getRPKSuperUserKey() types.NamespacedName {
	if a.getRPKSuperUser() == nil {
		return types.NamespacedName{}
	}
	return a.getRPKSuperUser().Key()
}

func (a *attachedResources) serviceAccount() {
	// if already initialized, exit immediately
	if _, ok := a.items[serviceAccount]; ok {
		return
	}
	a.items[serviceAccount] = resources.NewServiceAccount(a.reconciler.Client, a.cluster, a.reconciler.Scheme, a.log)
	a.order = append(a.order, serviceAccount)
}

func (a *attachedResources) getServiceAccount() *resources.ServiceAccountResource {
	a.serviceAccount()
	return a.items[serviceAccount].(*resources.ServiceAccountResource)
}

func (a *attachedResources) getServiceAccountKey() types.NamespacedName {
	return a.getServiceAccount().Key()
}

func (a *attachedResources) getServiceAccountName() string {
	return a.getServiceAccountKey().Name
}

func (a *attachedResources) secret() {
	// if already initialized, exit immediately
	if _, ok := a.items[secret]; ok {
		return
	}
	a.items[secret] = resources.PreStartStopScriptSecret(a.reconciler.Client, a.cluster, a.reconciler.Scheme, a.getHeadlessServiceFQDN(), a.getProxySuperUserKey(), a.getSchemaRegistrySuperUserKey(), a.log)
	a.order = append(a.order, secret)
}

func (a *attachedResources) statefulSet(cfg *clusterconfiguration.CombinedCfg) error {
	pki, err := a.getPKI()
	if err != nil {
		return err
	}

	nps, err := nodepools.GetNodePools(context.TODO(), a.cluster, a.reconciler.Client)
	if err != nil {
		return fmt.Errorf("while getting node pools: %w", err)
	}
	for _, np := range nps {
		stsKey := fmt.Sprintf("%s-%s", statefulSet, np.Name)
		if _, ok := a.items[stsKey]; ok {
			continue
		}

		a.items[stsKey] = resources.NewStatefulSet(
			a.reconciler.Client,
			a.cluster,
			a.reconciler.Scheme,
			a.getHeadlessServiceFQDN(),
			a.getHeadlessServiceName(),
			a.getNodeportServiceKey(),
			pki.StatefulSetVolumeProvider(),
			pki.AdminAPIConfigProvider(),
			a.getServiceAccountName(),
			a.reconciler.configuratorSettings,
			cfg,
			a.reconciler.AdminAPIClientFactory,
			a.reconciler.Dialer,
			a.reconciler.DecommissionWaitInterval,
			a.log,
			a.reconciler.MetricsTimeout,
			*np,
			a.autoDeletePVCs)

		a.order = append(a.order, stsKey)
	}

	return nil
}

func (a *attachedResources) getStatefulSet(cfg *clusterconfiguration.CombinedCfg) ([]*resources.StatefulSetResource, error) {
	if err := a.statefulSet(cfg); err != nil {
		return nil, err
	}
	out := make([]*resources.StatefulSetResource, 0)
	for k, sts := range a.items {
		if !strings.HasPrefix(k, statefulSet) || sts == nil {
			continue
		}

		out = append(out, sts.(*resources.StatefulSetResource))
	}
	return out, nil
}
