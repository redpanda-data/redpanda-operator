{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/post_install_upgrade_job.go" */ -}}

{{- define "redpanda.bootstrapTemplaterEnv" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $env := (get (fromJson (include "redpanda.TieredStorageCredentials.AsEnvVars" (dict "a" (list $state.Values.storage.tiered.credentialsSecretRef (get (fromJson (include "redpanda.Storage.GetTieredStorageConfig" (dict "a" (list $state.Values.storage)))) "r"))))) "r") -}}
{{- $_30_____additionalEnv := (get (fromJson (include "redpanda.ClusterConfiguration.Translate" (dict "a" (list (deepCopy $state.Values.config.extraClusterConfiguration))))) "r") -}}
{{- $_ := (index $_30_____additionalEnv 0) -}}
{{- $_ := (index $_30_____additionalEnv 1) -}}
{{- $additionalEnv := (index $_30_____additionalEnv 2) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (concat (default (list) $env) (default (list) $additionalEnv))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.bootstrapYamlTemplaterRenderer" -}}
{{- $state := (index .a 0) -}}
{{- $sts := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "Image" "" "InitImage" "" "SidecarImage" "" "CommonMounts" (coalesce nil) "Tuning" (coalesce nil) "DataDirOwnership" (coalesce nil) "FSValidator" (coalesce nil) "TieredStorageCacheOwnership" (coalesce nil) "Configurator" (coalesce nil) "Bootstrap" (coalesce nil)) (dict "SidecarImage" (printf `%s:%s` $sts.sideCars.image.repository $sts.sideCars.image.tag) "Bootstrap" (mustMergeOverwrite (dict "Env" (coalesce nil) "AdditionalCLIArgs" (coalesce nil)) (dict "Env" (get (fromJson (include "redpanda.bootstrapTemplaterEnv" (dict "a" (list $state)))) "r") "AdditionalCLIArgs" $state.Values.statefulset.initContainers.configurator.additionalCLIArgs))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.postInstallJobPodLabels" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (dict "app.kubernetes.io/name" (printf "%s-configuration" (get (fromJson (include "redpanda.Name" (dict "a" (list $state)))) "r")) "app.kubernetes.io/instance" $state.Release.Name "app.kubernetes.io/component" (printf "%.50s-post-install" (get (fromJson (include "redpanda.Name" (dict "a" (list $state)))) "r")))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.PostInstallUpgradeJob" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not $state.Values.post_install_job.enabled) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $image := (printf `%s:%s` $state.Values.statefulset.sideCars.image.repository $state.Values.statefulset.sideCars.image.tag) -}}
{{- $job := (mustMergeOverwrite (dict "metadata" (dict) "spec" (dict "template" (dict "metadata" (dict) "spec" (dict "containers" (coalesce nil)))) "status" (dict)) (mustMergeOverwrite (dict) (dict "apiVersion" "batch/v1" "kind" "Job")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (printf "%s-configuration" (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r")) "namespace" $state.Release.Namespace "labels" (merge (dict) (default (dict) $state.Values.post_install_job.labels) (get (fromJson (include "redpanda.FullLabels" (dict "a" (list $state)))) "r")) "annotations" (merge (dict) (default (dict) $state.Values.post_install_job.annotations) (dict "helm.sh/hook" "post-install,post-upgrade" "helm.sh/hook-delete-policy" "before-hook-creation" "helm.sh/hook-weight" "-5")))) "spec" (mustMergeOverwrite (dict "template" (dict "metadata" (dict) "spec" (dict "containers" (coalesce nil)))) (dict "template" (get (fromJson (include "redpanda.StrategicMergePatch" (dict "a" (list (get (fromJson (include "redpanda.StructuredTpl" (dict "a" (list $state $state.Values.post_install_job.podTemplate)))) "r") (get (fromJson (include "redpanda.StrategicMergePatch" (dict "a" (list (get (fromJson (include "redpanda.StructuredTpl" (dict "a" (list $state $state.Values.podTemplate)))) "r") (mustMergeOverwrite (dict "metadata" (dict) "spec" (dict "containers" (coalesce nil))) (dict "metadata" (mustMergeOverwrite (dict) (dict "generateName" (printf "%s-post-" $state.Release.Name) "labels" (merge (dict) (get (fromJson (include "redpanda.postInstallJobPodLabels" (dict "a" (list $state)))) "r") (default (dict) $state.Values.commonLabels)))) "spec" (mustMergeOverwrite (dict "containers" (coalesce nil)) (dict "restartPolicy" "Never" "initContainers" (get (fromJson (include "redpanda.StatefulSetInitContainerRenderer.Render" (dict "a" (list (deepCopy (get (fromJson (include "redpanda.bootstrapYamlTemplaterRenderer" (dict "a" (list $state $state.Values.statefulset)))) "r")))))) "r") "automountServiceAccountToken" false "containers" (list (mustMergeOverwrite (dict "name" "" "resources" (dict)) (dict "name" "post-install" "image" $image "env" (get (fromJson (include "redpanda.PostInstallUpgradeEnvironmentVariables" (dict "a" (list $state)))) "r") "command" (list "/redpanda-operator" "sync-cluster-config" "--users-directory" "/etc/secrets/users" "--redpanda-yaml" "/tmp/base-config/redpanda.yaml" "--bootstrap-yaml" "/tmp/config/.bootstrap.yaml") "volumeMounts" (concat (default (list) (get (fromJson (include "redpanda.CommonMounts" (dict "a" (list $state)))) "r")) (list (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "config" "mountPath" "/tmp/config")) (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "base-config" "mountPath" "/tmp/base-config"))))))) "volumes" (concat (default (list) (get (fromJson (include "redpanda.CommonVolumes" (dict "a" (list $state)))) "r")) (list (mustMergeOverwrite (dict "name" "") (mustMergeOverwrite (dict) (dict "configMap" (mustMergeOverwrite (dict) (mustMergeOverwrite (dict) (dict "name" (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r"))) (dict)))) (dict "name" "base-config")) (mustMergeOverwrite (dict "name" "") (mustMergeOverwrite (dict) (dict "emptyDir" (mustMergeOverwrite (dict) (dict)))) (dict "name" "config")))) "serviceAccountName" (get (fromJson (include "redpanda.ServiceAccountName" (dict "a" (list $state)))) "r"))))))))) "r"))))) "r"))))) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $job) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.PostInstallUpgradeEnvironmentVariables" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $envars := (list) -}}
{{- $license_1 := $state.Values.enterprise.license -}}
{{- $secretReference_2 := $state.Values.enterprise.licenseSecretRef -}}
{{- if (ne $license_1 "") -}}
{{- $envars = (concat (default (list) $envars) (list (mustMergeOverwrite (dict "name" "") (dict "name" "REDPANDA_LICENSE" "value" $license_1)))) -}}
{{- else -}}{{- if (ne (toJson $secretReference_2) "null") -}}
{{- $envars = (concat (default (list) $envars) (list (mustMergeOverwrite (dict "name" "") (dict "name" "REDPANDA_LICENSE" "valueFrom" (mustMergeOverwrite (dict) (dict "secretKeyRef" $secretReference_2)))))) -}}
{{- end -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "redpanda.bootstrapEnvVars" (dict "a" (list $state $envars)))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

