{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/chart/networkpolicy.go" */ -}}

{{- define "redpanda.NetworkPolicy" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $np := $state.Values.networkPolicy -}}
{{- if (not $np.enabled) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $brokers := (mustMergeOverwrite (dict) (dict "podSelector" (mustMergeOverwrite (dict) (dict "matchLabels" (get (fromJson (include "redpanda.ClusterPodLabelsSelector" (dict "a" (list $state)))) "r"))))) -}}
{{- $internal := (list (mustMergeOverwrite (dict) (dict "podSelector" (mustMergeOverwrite (dict) (dict "matchLabels" (dict "app.kubernetes.io/instance" $state.Release.Name)))))) -}}
{{- if (ne (toJson $np.operatorPeer) "null") -}}
{{- $internal = (concat (default (list) $internal) (list $np.operatorPeer)) -}}
{{- end -}}
{{- $clients := (mustMergeOverwrite (dict) (dict "ports" (get (fromJson (include "redpanda.networkPolicyPorts" (dict "a" (list (get (fromJson (include "redpanda.networkPolicyClientPorts" (dict "a" (list $state)))) "r"))))) "r"))) -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $np.clientPeers)))) "r") | int) (0 | int)) -}}
{{- $_ := (set $clients "from" (get (fromJson (include "redpanda.concatPeers" (dict "a" (list $internal $np.clientPeers)))) "r")) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict) "spec" (dict "podSelector" (dict))) (mustMergeOverwrite (dict) (dict "apiVersion" "networking.k8s.io/v1" "kind" "NetworkPolicy")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r") "namespace" $state.Release.Namespace "labels" (get (fromJson (include "redpanda.FullLabels" (dict "a" (list $state)))) "r") "annotations" (get (fromJson (include "redpanda.FullAnnotations" (dict "a" (list $state)))) "r"))) "spec" (mustMergeOverwrite (dict "podSelector" (dict)) (dict "podSelector" (mustMergeOverwrite (dict) (dict "matchLabels" (get (fromJson (include "redpanda.ClusterPodLabelsSelector" (dict "a" (list $state)))) "r"))) "policyTypes" (list "Ingress") "ingress" (list (mustMergeOverwrite (dict) (dict "ports" (get (fromJson (include "redpanda.networkPolicyPorts" (dict "a" (list (list ($state.Values.listeners.rpc.port | int)))))) "r") "from" (list $brokers))) (mustMergeOverwrite (dict) (dict "ports" (get (fromJson (include "redpanda.networkPolicyPorts" (dict "a" (list (list ($state.Values.listeners.admin.port | int)))))) "r") "from" (get (fromJson (include "redpanda.concatPeers" (dict "a" (list $internal $np.adminPeers)))) "r"))) $clients)))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.networkPolicyClientPorts" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $l := $state.Values.listeners -}}
{{- $ports := (list ($l.kafka.port | int) ($l.http.port | int) ($l.schemaRegistry.port | int)) -}}
{{- range $_, $ext := $l.admin.external -}}
{{- if (get (fromJson (include "redpanda.ExternalListener.IsEnabled" (dict "a" (list $ext)))) "r") -}}
{{- $ports = (concat (default (list) $ports) (list ($ext.port | int))) -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $ext := $l.http.external -}}
{{- if (get (fromJson (include "redpanda.ExternalListener.IsEnabled" (dict "a" (list $ext)))) "r") -}}
{{- $ports = (concat (default (list) $ports) (list ($ext.port | int))) -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $ext := $l.kafka.external -}}
{{- if (get (fromJson (include "redpanda.ExternalListener.IsEnabled" (dict "a" (list $ext)))) "r") -}}
{{- $ports = (concat (default (list) $ports) (list ($ext.port | int))) -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $ext := $l.schemaRegistry.external -}}
{{- if (get (fromJson (include "redpanda.ExternalListener.IsEnabled" (dict "a" (list $ext)))) "r") -}}
{{- $ports = (concat (default (list) $ports) (list ($ext.port | int))) -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $ports) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.networkPolicyPorts" -}}
{{- $ports := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $out := (list) -}}
{{- range $_, $p := $ports -}}
{{- $port := $p -}}
{{- $out = (concat (default (list) $out) (list (mustMergeOverwrite (dict) (dict "protocol" "TCP" "port" $port)))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $out) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.concatPeers" -}}
{{- $a := (index .a 0) -}}
{{- $b := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $out := (list) -}}
{{- $out = (concat (default (list) $out) (default (list) $a)) -}}
{{- $out = (concat (default (list) $out) (default (list) $b)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $out) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

