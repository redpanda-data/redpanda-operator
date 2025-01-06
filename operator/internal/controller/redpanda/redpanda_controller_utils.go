// Copyright 2025 Redpanda Data, Inc.
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
	"os"

	"github.com/fluxcd/pkg/runtime/logger"
	"github.com/go-logr/logr"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

const (
	K8sInstanceLabelKey  = "app.kubernetes.io/instance"
	K8sNameLabelKey      = "app.kubernetes.io/name"
	K8sComponentLabelKey = "app.kubernetes.io/component"
	K8sManagedByLabelKey = "app.kubernetes.io/managed-by"

	EnvHelmReleaseNameKey = "REDPANDA_HELM_RELEASE_NAME"
)

var UpdateEventFilter = predicate.Funcs{
	CreateFunc:  func(e event.CreateEvent) bool { return false },
	UpdateFunc:  func(e event.UpdateEvent) bool { return true },
	DeleteFunc:  func(e event.DeleteEvent) bool { return false },
	GenericFunc: func(e event.GenericEvent) bool { return false },
}

var DeleteEventFilter = predicate.Funcs{
	CreateFunc:  func(e event.CreateEvent) bool { return false },
	UpdateFunc:  func(e event.UpdateEvent) bool { return false },
	DeleteFunc:  func(e event.DeleteEvent) bool { return true },
	GenericFunc: func(e event.GenericEvent) bool { return false },
}

// Check to see if the release name of a helm chart matches the name of a redpanda object
// this is by design for the operator
func isValidReleaseName(releaseName string, redpandaNameList []string) bool {
	for i := range redpandaNameList {
		if releaseName == redpandaNameList[i] {
			return true
		}
	}
	return false
}

func getHelmValues(ctx context.Context, c client.Client, log logr.Logger, releaseName, namespace string, operatorMode bool) (map[string]interface{}, error) {
	if operatorMode {
		rp := v1alpha2.Redpanda{}
		err := c.Get(ctx, client.ObjectKey{Name: releaseName, Namespace: namespace}, &rp)
		if err != nil {
			return nil, fmt.Errorf("getting Redpanda customer resource: %w", err)
		}

		d, err := rp.GetDot(&rest.Config{})
		if err != nil {
			return nil, fmt.Errorf("compute chart values: %w", err)
		}

		return d.Values.AsMap(), nil
	}
	settings := cli.New()
	actionConfig := new(action.Configuration)
	if err := actionConfig.Init(settings.RESTClientGetter(), namespace, os.Getenv("HELM_DRIVER"), func(format string, v ...interface{}) { Debugf(log, format, v) }); err != nil {
		return nil, fmt.Errorf("could not create action-config for helm driver: %w", err)
	}

	gv := action.NewGetValues(actionConfig)
	gv.AllValues = true

	return gv.Run(releaseName)
}

func bestTrySetRetainPV(c client.Client, log logr.Logger, ctx context.Context, name, namespace string) {
	log.WithName("RedpandaNodePVCReconciler.bestTrySetRetainPV")
	pv := &corev1.PersistentVolume{}
	if getErr := c.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, pv); getErr != nil {
		Infof(log, "could not change retain policy of pv %s", pv.Name)
		return
	}
	// try to set reclaim policy, fail if we cannot set this to avoid data loss
	if pv.Spec.PersistentVolumeReclaimPolicy != corev1.PersistentVolumeReclaimRetain {
		pv.Spec.PersistentVolumeReclaimPolicy = corev1.PersistentVolumeReclaimRetain
		if updateErr := c.Update(ctx, pv); updateErr != nil {
			// no need to place error here. we simply move on and not attempt to remove the pv
			Infof(log, "could not set reclaim policy for %s; continuing: %s", pv.Name, updateErr.Error())
		}
	}
}

func Infof(log logr.Logger, format string, a ...interface{}) {
	log.Info(fmt.Sprintf(format, a...))
}

func Debugf(log logr.Logger, format string, a ...interface{}) {
	log.V(logger.DebugLevel).Info(fmt.Sprintf(format, a...))
}

func Tracef(log logr.Logger, format string, a ...interface{}) {
	log.V(logger.TraceLevel).Info(fmt.Sprintf(format, a...))
}
