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
	"crypto/rand"
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/clusterconfiguration"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/labels"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources/featuregates"
)

const (
	baseSuffix                  = "base"
	dataDirectory               = "/var/lib/redpanda/data"
	archivalCacheIndexDirectory = "/var/lib/shadow-index-cache"

	superusersConfigurationKey = "superusers"

	oneMB          = 1024 * 1024
	logSegmentSize = 512 * oneMB

	saslMechanism = "SCRAM-SHA-256"

	configKey                  = "redpanda.yaml"
	bootstrapConfigFile        = ".bootstrap.yaml"
	bootstrapTemplateEnvVar    = "BOOTSTRAP_TEMPLATE"
	bootstrapDestinationEnvVar = "BOOTSTRAP_DESTINATION"
	bootstrapTemplateFile      = ".bootstrap.json.in"
)

// LastAppliedCriticalConfigurationAnnotationKey is used to store the hash of the most-recently-applied configuration,
// selecting only those values which are marked in the schema as requiring a cluster restart.
var LastAppliedCriticalConfigurationAnnotationKey = vectorizedv1alpha1.GroupVersion.Group + "/last-applied-critical-configuration"

var _ Resource = &ConfigMapResource{}

// ConfigMapResource contains definition and reconciliation logic for operator's ConfigMap.
// The ConfigMap contains the configuration as well as init script.
type ConfigMapResource struct {
	k8sclient.Client
	scheme       *runtime.Scheme
	pandaCluster *vectorizedv1alpha1.Cluster

	cfg    *clusterconfiguration.CombinedCfg
	logger logr.Logger
}

// NewConfigMap creates ConfigMapResource
func NewConfigMap(
	client k8sclient.Client,
	pandaCluster *vectorizedv1alpha1.Cluster,
	scheme *runtime.Scheme,
	cfg *clusterconfiguration.CombinedCfg,
	logger logr.Logger,
) *ConfigMapResource {
	return &ConfigMapResource{
		client,
		scheme,
		pandaCluster,
		cfg,
		logger,
	}
}

// Ensure will manage kubernetes v1.ConfigMap for redpanda.vectorized.io CR
func (r *ConfigMapResource) Ensure(ctx context.Context) error {
	obj, err := r.obj(ctx)
	if err != nil {
		return fmt.Errorf("unable to construct object: %w", err)
	}
	created, err := CreateIfNotExists(ctx, r, obj, r.logger)
	if err != nil || created {
		return err
	}
	var cm corev1.ConfigMap
	err = r.Get(ctx, r.Key(), &cm)
	if err != nil {
		return fmt.Errorf("error while fetching ConfigMap resource: %w", err)
	}

	return r.update(ctx, &cm, obj.(*corev1.ConfigMap), r.Client, r.logger)
}

func (r *ConfigMapResource) update(
	ctx context.Context,
	current *corev1.ConfigMap,
	modified *corev1.ConfigMap,
	c k8sclient.Client,
	logger logr.Logger,
) error {
	// Do not touch existing last-applied-configuration (it's not reconciled in the main loop)
	if val, ok := current.Annotations[LastAppliedCriticalConfigurationAnnotationKey]; ok {
		if modified.Annotations == nil {
			modified.Annotations = make(map[string]string)
		}
		modified.Annotations[LastAppliedCriticalConfigurationAnnotationKey] = val
	}

	if err := r.markConfigurationConditionChanged(ctx, current, modified); err != nil {
		return err
	}

	_, err := Update(ctx, current, modified, c, logger)
	return err
}

// markConfigurationConditionChanged verifies and marks the cluster as needing synchronization (using the ClusterConfigured condition).
// The condition is changed so that the configuration controller can later restore it back to normal after interacting with the cluster.
func (r *ConfigMapResource) markConfigurationConditionChanged(
	ctx context.Context, current *corev1.ConfigMap, modified *corev1.ConfigMap,
) error {
	if !featuregates.CentralizedConfiguration(r.pandaCluster.Spec.Version) {
		return nil
	}

	status := r.pandaCluster.Status.GetConditionStatus(vectorizedv1alpha1.ClusterConfiguredConditionType)
	if status == corev1.ConditionFalse {
		// Condition already indicates a change
		return nil
	}

	// If the condition is not present, or it does not currently indicate a change, we check it again
	if !r.globalConfigurationChanged(current, modified) {
		return nil
	}

	r.logger.Info("Detected configuration change in the cluster")

	// We need to mark the cluster as changed to trigger the configuration workflow
	r.pandaCluster.Status.SetCondition(
		vectorizedv1alpha1.ClusterConfiguredConditionType,
		corev1.ConditionFalse,
		vectorizedv1alpha1.ClusterConfiguredReasonUpdating,
		"Detected cluster configuration change that needs to be applied to the cluster",
	)
	return r.Status().Update(ctx, r.pandaCluster)
}

// obj returns resource managed client.Object
func (r *ConfigMapResource) obj(context.Context) (k8sclient.Object, error) {
	cfgSerialized, err := r.cfg.Templates()
	if err != nil {
		return nil, fmt.Errorf("serializing: %w", err)
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: r.Key().Namespace,
			Name:      r.Key().Name,
			Labels:    labels.ForCluster(r.pandaCluster),
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		Data: cfgSerialized,
	}

	err = controllerutil.SetControllerReference(r.pandaCluster, cm, r.scheme)
	if err != nil {
		return nil, fmt.Errorf("setting controller reference: %w", err)
	}

	return cm, nil
}

// Key returns namespace/name object that is used to identify object.
// For reference please visit types.NamespacedName docs in k8s.io/apimachinery
func (r *ConfigMapResource) Key() types.NamespacedName {
	return ConfigMapKey(r.pandaCluster)
}

// ConfigMapKey provides config map name that derived from redpanda.vectorized.io CR
func ConfigMapKey(pandaCluster *vectorizedv1alpha1.Cluster) types.NamespacedName {
	return types.NamespacedName{Name: resourceNameTrim(pandaCluster.Name, baseSuffix), Namespace: pandaCluster.Namespace}
}

// TODO move to utilities
var letters = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func generatePassword(length int) (string, error) {
	pwdBytes := make([]byte, length)

	if _, err := rand.Read(pwdBytes); err != nil {
		return "", err
	}

	for i, b := range pwdBytes {
		pwdBytes[i] = letters[b%byte(len(letters))]
	}

	return string(pwdBytes), nil
}

// globalConfigurationChanged verifies if the new global configuration
// is different from the one in the previous version of the ConfigMap
func (r *ConfigMapResource) globalConfigurationChanged(
	current *corev1.ConfigMap, modified *corev1.ConfigMap,
) bool {
	if !featuregates.CentralizedConfiguration(r.pandaCluster.Spec.Version) {
		return false
	}

	// TODO: this is a short-term change; it won't detect a change in any referenced secrets
	oldConfigNode := current.Data[configKey]
	oldConfigBootstrap := current.Data[bootstrapTemplateFile]

	newConfigNode := modified.Data[configKey]
	newConfigBootstrap := modified.Data[bootstrapTemplateFile]

	return newConfigNode != oldConfigNode || newConfigBootstrap != oldConfigBootstrap
}

// GetAnnotationFromCluster returns the last applied configuration from the configmap,
// together with information about the presence of the configmap itself.
func (r *ConfigMapResource) GetAnnotationFromCluster(
	ctx context.Context,
	annotation string,
) (annotationValue *string, configmapExists bool, err error) {
	existing := corev1.ConfigMap{}
	if err := r.Client.Get(ctx, r.Key(), &existing); err != nil {
		if apierrors.IsNotFound(err) {
			// No keys have been used previously
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("could not load configmap for reading last applied configuration: %w", err)
	}
	if ann, ok := existing.Annotations[annotation]; ok {
		return &ann, true, nil
	}
	return nil, true, nil
}

// SetAnnotationForCluster sets or updates an annotation in the configmap
func (r *ConfigMapResource) SetAnnotationForCluster(
	ctx context.Context, annotation string, newValue *string,
) error {
	existing := corev1.ConfigMap{}
	if err := r.Client.Get(ctx, r.Key(), &existing); err != nil {
		return fmt.Errorf("could not load configmap for storing last applied configuration: %w", err)
	}
	existingValue, found := existing.Annotations[annotation]
	if newValue == nil {
		if !found {
			return nil
		}
		delete(existing.Annotations, annotation)
	} else {
		if existingValue == *newValue {
			return nil
		}
		existing.Annotations[annotation] = *newValue
	}
	return r.Update(ctx, &existing)
}
