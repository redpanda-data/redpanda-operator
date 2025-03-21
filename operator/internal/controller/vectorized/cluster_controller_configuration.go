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
	"net/http"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/go-logr/logr"
	"github.com/redpanda-data/common-go/rpadmin"
	corev1 "k8s.io/api/core/v1"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/cmd/syncclusterconfig"
	adminutils "github.com/redpanda-data/redpanda-operator/operator/pkg/admin"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources/certmanager"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources/configuration"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources/featuregates"
)

const (
	defaultConfigurationReassertionPeriod = time.Minute
)

func (r *ClusterReconciler) configurationReassertionPeriod() time.Duration {
	if r.ConfigurationReassertionPeriod == 0 {
		return defaultConfigurationReassertionPeriod
	}
	return r.ConfigurationReassertionPeriod
}

// reconcileConfiguration ensures that the cluster configuration is synchronized with expected data
//
//nolint:funlen // splitting makes it difficult to follow
func (r *ClusterReconciler) reconcileConfiguration(
	ctx context.Context,
	redpandaCluster *vectorizedv1alpha1.Cluster,
	configMapResource *resources.ConfigMapResource,
	statefulSetResources []*resources.StatefulSetResource,
	pki *certmanager.PkiReconciler,
	fqdn string,
	l logr.Logger,
) (time.Duration, error) {
	log := l.WithName("reconcileConfiguration")
	errorWithContext := newErrorWithContext(redpandaCluster.Namespace, redpandaCluster.Name)
	if !featuregates.CentralizedConfiguration(redpandaCluster.Spec.Version) {
		log.Info("Cluster is not using centralized configuration, skipping...")
		return 0, nil
	}

	if added, err := r.ensureConditionPresent(ctx, redpandaCluster, log); err != nil || added {
		// If condition is added or error returned, we wait for another reconcile loop
		return 0, err
	}

	if delay := r.ratelimitCondition(redpandaCluster, vectorizedv1alpha1.ClusterConfiguredConditionType); delay > 0 {
		log.Info("Waiting to reassert cluster configuration")
		return delay, nil
	}

	config, err := configMapResource.CreateConfiguration(ctx)
	if err != nil {
		return 0, errorWithContext(err, "error while creating the configuration")
	}

	adminAPI, err := r.AdminAPIClientFactory(ctx, r, redpandaCluster, fqdn, pki.AdminAPIConfigProvider(), r.Dialer)
	if err != nil {
		return 0, errorWithContext(err, "error creating the admin API client")
	}

	schema, _, _, err := r.retrieveClusterState(ctx, redpandaCluster, adminAPI)
	if err != nil {
		return 0, err
	}

	lastAppliedCriticalConfigurationHash, err := r.getOrInitLastAppliedCriticalConfiguration(ctx, configMapResource, config, redpandaCluster.Namespace, schema)
	if err != nil {
		return 0, errorWithContext(err, "could not load the last applied configuration")
	}

	// Checking if the feature is active because in the initial stages of cluster creation, it takes time for the feature to be activated
	// and the API returns the same error (400) that is returned in case of malformed input, which causes a stop of the reconciliation
	var centralConfigActive bool
	if centralConfigActive, err = adminutils.IsFeatureActive(ctx, adminAPI, adminutils.CentralConfigFeatureName); err != nil {
		return 0, errorWithContext(err, "could not determine if central config is active in the cluster")
	} else if !centralConfigActive {
		log.Info("Waiting for the centralized configuration feature to be active in the cluster")
		return 0, &resources.RequeueAfterError{
			RequeueAfter: resources.RequeueDuration,
			Msg:          "centralized configuration feature not active",
		}
	}

	patchSuccess, err := r.applyPatchIfNeeded(ctx, redpandaCluster, adminAPI, config, schema, log)
	if err != nil || !patchSuccess {
		// patchSuccess=false indicates an error set on the condition that should not be propagated (but we terminate reconciliation anyway)
		return 0, err
	}

	// TODO a failure and restart here (after successful patch, before setting the last applied configuration) may lead to inconsistency if the user
	// changes the CR in the meantime (e.g. removing a field), since we applied a config to the cluster but did not store the information anywhere else.
	// A possible fix is doing a two-phase commit (first stage commit on configmap, then apply it to the cluster, with possibility to recover on failure),
	// but it seems overkill given that the case is rare and requires cooperation from the user.

	for _, statefulSetResource := range statefulSetResources {
		if statefulSetResource == nil {
			continue
		}
		hash, hashChanged, err := r.checkCentralizedConfigurationHashChange(ctx, redpandaCluster, config, schema, lastAppliedCriticalConfigurationHash, statefulSetResource)
		if err != nil {
			return 0, err
		} else if hashChanged {
			// Definitely needs restart
			log.Info("Centralized configuration hash has changed")
			if err = statefulSetResource.SetCentralizedConfigurationHashInCluster(ctx, hash); err != nil {
				return 0, errorWithContext(err, "could not update config hash on statefulset")
			}
		}
	}

	// Now we can mark the new lastAppliedCriticalConfiguration for next update
	hash, err := config.GetCentralizedConfigurationHash(ctx, r.Client, r.CloudSecretsExpander ,schema, redpandaCluster.Namespace)
	if err != nil {
		return 0, errorWithContext(err, "could not concretize critical configuration to store last applied configuration in the cluster")
	}
	if err = configMapResource.SetAnnotationForCluster(ctx, resources.LastAppliedCriticalConfigurationAnnotationKey, &hash); err != nil {
		return 0, errorWithContext(err, "could not store last applied configuration in the cluster")
	}

	// Synchronized status with cluster, including triggering a restart if needed
	conditionData, err := r.synchronizeStatusWithCluster(ctx, redpandaCluster, statefulSetResources, adminAPI, log)
	if err != nil {
		return 0, err
	}

	// If condition is not met, we need to reschedule, waiting for the cluster to heal.
	if conditionData.Status != corev1.ConditionTrue {
		return 0, &resources.RequeueAfterError{
			RequeueAfter: resources.RequeueDuration,
			Msg:          fmt.Sprintf("cluster configuration is not in sync (%s): %s", conditionData.Reason, conditionData.Message),
		}
	}

	return 0, nil
}

// ratelimitCondition ensures that the reassertion of cluster configuration is done
// once every minute or so, but no more rapidly than that.
// (This is modelled on the v2 operator - we should look to merge these utilities.)
// Rather than boolean blindness, if we should rate-limit then this will return a
// non-zero minimum wait duration.
func (r *ClusterReconciler) ratelimitCondition(rp *vectorizedv1alpha1.Cluster, conditionType vectorizedv1alpha1.ClusterConditionType) time.Duration {
	upToDate := rp.Status.ObservedGeneration != 0 && rp.Status.ObservedGeneration == rp.Generation
	if !upToDate {
		return 0
	}

	cond := rp.Status.GetCondition(conditionType)
	if cond == nil {
		return 0
	}
	if cond.Status != corev1.ConditionTrue {
		return 0
	}

	recheckAfter := r.configurationReassertionPeriod() - time.Since(cond.LastTransitionTime.Time)
	return max(0, recheckAfter)
}

// getOrInitLastAppliedCriticalConfiguration gets the last applied critical configuration hash to the cluster or creates it when missing.
//
// This is needed because the controller will later use that annotation to drive the restart of stateful sets.
// A missing annotation indicates a cluster where centralized configuration has just been primed using the
// contents of the .bootstrap.yaml file, so we freeze its current content (early in the reconciliation cycle) so that
// subsequent patches are computed correctly.
func (r *ClusterReconciler) getOrInitLastAppliedCriticalConfiguration(
	ctx context.Context,
	configMapResource *resources.ConfigMapResource,
	config *configuration.GlobalConfiguration,
	namespace string,
	schema rpadmin.ConfigSchema,
) (string, error) {
	lastApplied, cmPresent, err := configMapResource.GetAnnotationFromCluster(ctx, resources.LastAppliedCriticalConfigurationAnnotationKey)
	if err != nil {
		return "", err
	}
	if !cmPresent || lastApplied != nil {
		return *lastApplied, nil
	}

	hash, err := config.GetCentralizedConfigurationHash(ctx, r.Client, r.CloudSecretsExpander, schema, namespace)
	if err != nil {
		return "", err
	}
	if err := configMapResource.SetAnnotationForCluster(ctx, resources.LastAppliedCriticalConfigurationAnnotationKey, &hash); err != nil {
		return "", err
	}
	return hash, nil
}

func (r *ClusterReconciler) applyPatchIfNeeded(
	ctx context.Context,
	redpandaCluster *vectorizedv1alpha1.Cluster,
	adminAPI adminutils.AdminAPIClient,
	cfg *configuration.GlobalConfiguration,
	schema rpadmin.ConfigSchema,
	l logr.Logger,
) (success bool, err error) {
	log := l.WithName("applyPatchIfNeeded")
	errorWithContext := newErrorWithContext(redpandaCluster.Namespace, redpandaCluster.Name)

	// Massage the clusterConfig into an appropriate set of values.
	// Because we're going to perform a declarative application, invalid values will either
	// be overwritten (if they're supplied in the cfg), or removed (if they're not).
	// No additional handling is needed.
	properties, err := cfg.ConcreteConfiguration(ctx, r, r.CloudSecretsExpander, redpandaCluster.Namespace, schema)
	if err != nil {
		return false, err
	}

	// Unconditionally apply the update
	syncer := syncclusterconfig.Syncer{
		Client: adminAPI,
		Mode:   syncclusterconfig.SyncerModeDeclarative,
		EqualityCheck: func(key string, desired, current any) bool {
			return configuration.PropertiesEqual(log, desired, current, schema[key])
		},
	}
	// The updated config_version is logged by syncer
	err = syncer.Sync(ctx, properties, nil)
	if err != nil {
		var conditionData *vectorizedv1alpha1.ClusterCondition
		conditionData, err = tryMapErrorToCondition(err)
		if err != nil {
			return false, errorWithContext(err, "could not patch centralized configuration")
		}
		log.Info("Failure when patching the configuration using the admin API")
		conditionChanged := redpandaCluster.Status.SetCondition(
			conditionData.Type,
			conditionData.Status,
			conditionData.Reason,
			conditionData.Message,
		)
		if conditionChanged {
			log.Info("Updating the condition with failure information",
				"status", conditionData.Status,
				"reason", conditionData.Reason,
				"message", conditionData.Message,
			)
			if err := r.Status().Update(ctx, redpandaCluster); err != nil {
				return false, errorWithContext(err, "could not update condition on cluster")
			}
		}
		// Patch issue is due to user error, so it's unrecoverable
		return false, nil
	}
	return true, nil
}

func (r *ClusterReconciler) retrieveClusterState(
	ctx context.Context,
	redpandaCluster *vectorizedv1alpha1.Cluster,
	adminAPI adminutils.AdminAPIClient,
) (rpadmin.ConfigSchema, rpadmin.Config, rpadmin.ConfigStatusResponse, error) {
	errorWithContext := newErrorWithContext(redpandaCluster.Namespace, redpandaCluster.Name)

	schema, err := adminAPI.ClusterConfigSchema(ctx)
	if err != nil {
		return nil, nil, nil, errorWithContext(err, "could not get centralized configuration schema")
	}
	clusterConfig, err := adminAPI.Config(ctx, true)
	if err != nil {
		return nil, nil, nil, errorWithContext(err, "could not get current centralized configuration from cluster")
	}

	// We always send requests for config status to the leader to avoid inconsistencies due to config propagation delays.
	status, err := adminAPI.ClusterConfigStatus(ctx, true)
	if err != nil {
		return nil, nil, nil, errorWithContext(err, "could not get current centralized configuration status from cluster")
	}

	return schema, clusterConfig, status, nil
}

func (r *ClusterReconciler) ensureConditionPresent(
	ctx context.Context,
	redpandaCluster *vectorizedv1alpha1.Cluster,
	l logr.Logger,
) (bool, error) {
	log := l.WithName("ensureConditionPresent")
	if condition := redpandaCluster.Status.GetCondition(vectorizedv1alpha1.ClusterConfiguredConditionType); condition == nil {
		// nil condition means that no change has been detected earlier, but we can't assume that configuration is in sync
		// because of multiple reasons, for example:
		// - .bootstrap.yaml may contain invalid/unknown properties
		// - The PVC may have been recycled from a previously running cluster with a different configuration
		log.Info("Setting the condition to false until check against admin API")
		redpandaCluster.Status.SetCondition(
			vectorizedv1alpha1.ClusterConfiguredConditionType,
			corev1.ConditionFalse,
			vectorizedv1alpha1.ClusterConfiguredReasonUpdating,
			"Verifying configuration using cluster admin API",
		)
		if err := r.Status().Update(ctx, redpandaCluster); err != nil {
			return false, newErrorWithContext(redpandaCluster.Namespace, redpandaCluster.Name)(err, "could not update condition on cluster")
		}
		return true, nil
	}
	return false, nil
}

func (r *ClusterReconciler) checkCentralizedConfigurationHashChange(
	ctx context.Context,
	redpandaCluster *vectorizedv1alpha1.Cluster,
	config *configuration.GlobalConfiguration,
	schema rpadmin.ConfigSchema,
	lastAppliedHash string,
	statefulSetResource *resources.StatefulSetResource,
) (hash string, changed bool, err error) {
	hash, err = config.GetCentralizedConfigurationHash(ctx, r.Client, r.CloudSecretsExpander, schema, redpandaCluster.Namespace)
	if err != nil {
		return "", false, newErrorWithContext(redpandaCluster.Namespace, redpandaCluster.Name)(err, "could not compute hash of the new configuration")
	}

	oldHash, err := statefulSetResource.GetCentralizedConfigurationHashFromCluster(ctx)
	if err != nil {
		return "", false, err
	}

	if oldHash == "" {
		// Annotation not yet set on the statefulset (e.g. first time we change config).
		// We check a diff against last applied configuration to avoid triggering a restart when not needed.
		oldHash = lastAppliedHash
	}

	return hash, hash != oldHash, nil
}

func (r *ClusterReconciler) synchronizeStatusWithCluster(
	ctx context.Context,
	redpandaCluster *vectorizedv1alpha1.Cluster,
	statefulsets []*resources.StatefulSetResource,
	adminAPI adminutils.AdminAPIClient,
	l logr.Logger,
) (*vectorizedv1alpha1.ClusterCondition, error) {
	log := l.WithName("synchronizeStatusWithCluster")
	errorWithContext := newErrorWithContext(redpandaCluster.Namespace, redpandaCluster.Name)
	// Check status again on the leader using admin API
	status, err := adminAPI.ClusterConfigStatus(ctx, true)
	if err != nil {
		return nil, errorWithContext(err, "could not get config status from admin API")
	}
	conditionData := mapStatusToCondition(status)
	conditionChanged := redpandaCluster.Status.SetCondition(conditionData.Type, conditionData.Status, conditionData.Reason, conditionData.Message)
	clusterNeedsRestart := needsRestart(status, log)
	clusterSafeToRestart := isSafeToRestart(status, log)
	restartingCluster := clusterNeedsRestart && clusterSafeToRestart
	isRestarting := redpandaCluster.Status.IsRestarting()

	log.Info("Synchronizing configuration state for cluster",
		"status", conditionData.Status,
		"reason", conditionData.Reason,
		"message", conditionData.Message,
		"needs_restart", clusterNeedsRestart,
		"restarting", restartingCluster,
	)
	if conditionChanged || (restartingCluster && !isRestarting) {
		log.Info("Updating configuration state for cluster")
		// Trigger restart here if needed and safe to do it
		if restartingCluster {
			redpandaCluster.Status.SetRestarting(true)
		}

		if err := r.Status().Update(ctx, redpandaCluster); err != nil {
			return nil, errorWithContext(err, "could not update condition on cluster")
		}
	}
	if restartingCluster && !isRestarting {
		for _, sts := range statefulsets {
			if sts == nil {
				continue
			}
			if err := sts.MarkPodsForUpdate(ctx); err != nil {
				return nil, errorWithContext(err, "could not mark pods for update")
			}
		}
	}
	return redpandaCluster.Status.GetCondition(conditionData.Type), nil
}

//nolint:gocritic // I like this if else chain
func mapStatusToCondition(
	clusterStatus rpadmin.ConfigStatusResponse,
) vectorizedv1alpha1.ClusterCondition {
	var condition *vectorizedv1alpha1.ClusterCondition
	var configVersion int64 = -1
	for _, nodeStatus := range clusterStatus {
		if len(nodeStatus.Invalid) > 0 {
			condition = &vectorizedv1alpha1.ClusterCondition{
				Type:    vectorizedv1alpha1.ClusterConfiguredConditionType,
				Status:  corev1.ConditionFalse,
				Reason:  vectorizedv1alpha1.ClusterConfiguredReasonError,
				Message: fmt.Sprintf("Invalid value provided for properties: %s", strings.Join(nodeStatus.Invalid, ", ")),
			}
		} else if len(nodeStatus.Unknown) > 0 {
			condition = &vectorizedv1alpha1.ClusterCondition{
				Type:    vectorizedv1alpha1.ClusterConfiguredConditionType,
				Status:  corev1.ConditionFalse,
				Reason:  vectorizedv1alpha1.ClusterConfiguredReasonError,
				Message: fmt.Sprintf("Unknown properties: %s", strings.Join(nodeStatus.Unknown, ", ")),
			}
		} else if nodeStatus.Restart {
			condition = &vectorizedv1alpha1.ClusterCondition{
				Type:    vectorizedv1alpha1.ClusterConfiguredConditionType,
				Status:  corev1.ConditionFalse,
				Reason:  vectorizedv1alpha1.ClusterConfiguredReasonUpdating,
				Message: fmt.Sprintf("Node %d needs restart", nodeStatus.NodeID),
			}
		} else if configVersion >= 0 && nodeStatus.ConfigVersion != configVersion {
			condition = &vectorizedv1alpha1.ClusterCondition{
				Type:    vectorizedv1alpha1.ClusterConfiguredConditionType,
				Status:  corev1.ConditionFalse,
				Reason:  vectorizedv1alpha1.ClusterConfiguredReasonUpdating,
				Message: fmt.Sprintf("Not all nodes share the same configuration version: %d / %d", nodeStatus.ConfigVersion, configVersion),
			}
		}

		configVersion = nodeStatus.ConfigVersion
	}

	if condition == nil {
		// Everything is ok
		condition = &vectorizedv1alpha1.ClusterCondition{
			Type:    vectorizedv1alpha1.ClusterConfiguredConditionType,
			Status:  corev1.ConditionTrue,
			Message: fmt.Sprintf("Cluster configuration reasserted at %s", time.Now().UTC().Format(time.DateTime)),
		}
	}
	return *condition
}

func needsRestart(
	clusterStatus rpadmin.ConfigStatusResponse, l logr.Logger,
) bool {
	log := l.WithName("needsRestart")
	nodeNeedsRestart := false
	for i := range clusterStatus {
		log.WithValues("broker id", clusterStatus[i].NodeID, "restart status", clusterStatus[i].Restart).Info("broker restart status")
		if clusterStatus[i].Restart {
			nodeNeedsRestart = true
		}
	}
	return nodeNeedsRestart
}

func isSafeToRestart(
	clusterStatus rpadmin.ConfigStatusResponse, l logr.Logger,
) bool {
	log := l.WithName("isSafeToRestart")
	configVersions := make(map[int64]bool)
	for i := range clusterStatus {
		log.Info(fmt.Sprintf("Node %d is using config version %d", clusterStatus[i].NodeID, clusterStatus[i].ConfigVersion))
		configVersions[clusterStatus[i].ConfigVersion] = true
	}
	return len(configVersions) == 1
}

// tryMapErrorToCondition tries to map validation errors received from the cluster to a condition
// or returns the same error if not possible.
func tryMapErrorToCondition(
	err error,
) (*vectorizedv1alpha1.ClusterCondition, error) {
	var httpErr *rpadmin.HTTPResponseError
	if errors.As(err, &httpErr) {
		if httpErr.Response != nil && httpErr.Response.StatusCode == http.StatusBadRequest {
			return &vectorizedv1alpha1.ClusterCondition{
				Type:    vectorizedv1alpha1.ClusterConfiguredConditionType,
				Status:  corev1.ConditionFalse,
				Reason:  vectorizedv1alpha1.ClusterConfiguredReasonError,
				Message: string(httpErr.Body),
			}, nil
		}
	}
	return nil, err
}

func newErrorWithContext(namespace, name string) func(error, string) error {
	return func(err error, msg string) error {
		return fmt.Errorf("%s (cluster %s/%s): %w", msg, namespace, name, err)
	}
}
