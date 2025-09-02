{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/notes.go" */ -}}

{{- define "redpandav25.Warnings" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $warnings := (coalesce nil) -}}
{{- $w_1 := (get (fromJson (include "redpandav25.cpuWarning" (dict "a" (list $state)))) "r") -}}
{{- if (ne $w_1 "") -}}
{{- $warnings = (concat (default (list) $warnings) (list (printf `**Warning**: %s` $w_1))) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $warnings) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpandav25.cpuWarning" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $coresInMillis := ((get (fromJson (include "_shims.resource_MilliValue" (dict "a" (list $state.Values.resources.cpu.cores)))) "r") | int64) -}}
{{- if (lt $coresInMillis (1000 | int64)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (printf "%dm is below the minimum recommended CPU value for Redpanda" $coresInMillis)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" "") | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpandav25.Notes" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $anySASL := (get (fromJson (include "redpandav25.Auth.IsSASLEnabled" (dict "a" (list $state.Values.auth)))) "r") -}}
{{- $notes := (coalesce nil) -}}
{{- $notes = (concat (default (list) $notes) (list `` `` `` `` (printf `Congratulations on installing %s!` $state.Chart.Name) `` `The pods will rollout in a few seconds. To check the status:` `` (printf `  kubectl -n %s rollout status statefulset %s --watch` $state.Release.Namespace (get (fromJson (include "redpandav25.Fullname" (dict "a" (list $state)))) "r")))) -}}
{{- if (and $state.Values.external.enabled (eq $state.Values.external.type "LoadBalancer")) -}}
{{- $notes = (concat (default (list) $notes) (list `` `If you are using the load balancer service with a cloud provider, the services will likely have automatically-generated addresses. In this scenario the advertised listeners must be updated in order for external access to work. Run the following command once Redpanda is deployed:` `` (printf `  helm upgrade %s redpanda/redpanda --reuse-values -n %s --set $(kubectl get svc -n %s -o jsonpath='{"external.addresses={"}{ range .items[*]}{.status.loadBalancer.ingress[0].ip }{.status.loadBalancer.ingress[0].hostname}{","}{ end }{"}\n"}')` (get (fromJson (include "redpandav25.Name" (dict "a" (list $state)))) "r") $state.Release.Namespace $state.Release.Namespace))) -}}
{{- end -}}
{{- $profiles := (keys $state.Values.listeners.kafka.external) -}}
{{- $_ := (sortAlpha $profiles) -}}
{{- $profileName := (index $profiles (0 | int)) -}}
{{- $notes = (concat (default (list) $notes) (list `` `Set up rpk for access to your external listeners:`)) -}}
{{- $profile := (ternary (index $state.Values.listeners.kafka.external $profileName) (dict "enabled" (coalesce nil) "advertisedPorts" (coalesce nil) "port" 0 "nodePort" (coalesce nil) "tls" (coalesce nil)) (hasKey $state.Values.listeners.kafka.external $profileName)) -}}
{{- if (get (fromJson (include "redpandav25.TLSEnabled" (dict "a" (list $state)))) "r") -}}
{{- $external := "" -}}
{{- if (and (ne (toJson $profile.tls) "null") (ne (toJson $profile.tls.cert) "null")) -}}
{{- $external = $profile.tls.cert -}}
{{- else -}}
{{- $external = $state.Values.listeners.kafka.tls.cert -}}
{{- end -}}
{{- $notes = (concat (default (list) $notes) (list (printf `  kubectl get secret -n %s %s-%s-cert -o go-template='{{ index .data "ca.crt" | base64decode }}' > ca.crt` $state.Release.Namespace (get (fromJson (include "redpandav25.Fullname" (dict "a" (list $state)))) "r") $external))) -}}
{{- if (or $state.Values.listeners.kafka.tls.requireClientAuth $state.Values.listeners.admin.tls.requireClientAuth) -}}
{{- $notes = (concat (default (list) $notes) (list (printf `  kubectl get secret -n %s %s-client -o go-template='{{ index .data "tls.crt" | base64decode }}' > tls.crt` $state.Release.Namespace (get (fromJson (include "redpandav25.Fullname" (dict "a" (list $state)))) "r")) (printf `  kubectl get secret -n %s %s-client -o go-template='{{ index .data "tls.key" | base64decode }}' > tls.key` $state.Release.Namespace (get (fromJson (include "redpandav25.Fullname" (dict "a" (list $state)))) "r")))) -}}
{{- end -}}
{{- end -}}
{{- $notes = (concat (default (list) $notes) (list (printf `  rpk profile create --from-profile <(kubectl get configmap -n %s %s-rpk -o go-template='{{ .data.profile }}') %s` $state.Release.Namespace (get (fromJson (include "redpandav25.Fullname" (dict "a" (list $state)))) "r") $profileName) `` `Set up dns to look up the pods on their Kubernetes Nodes. You can use this query to get the list of short-names to IP addresses. Add your external domain to the hostnames and you could test by adding these to your /etc/hosts:` `` (printf `  kubectl get pod -n %s -o custom-columns=node:.status.hostIP,name:.metadata.name --no-headers -l app.kubernetes.io/name=redpanda,app.kubernetes.io/component=redpanda-statefulset` $state.Release.Namespace))) -}}
{{- if $anySASL -}}
{{- $notes = (concat (default (list) $notes) (list `` `Set the credentials in the environment:` `` (printf `  kubectl -n %s get secret %s -o go-template="{{ range .data }}{{ . | base64decode }}{{ end }}" | IFS=: read -r %s` $state.Release.Namespace $state.Values.auth.sasl.secretRef (get (fromJson (include "redpandav25.RpkSASLEnvironmentVariables" (dict "a" (list $state)))) "r")) (printf `  export %s` (get (fromJson (include "redpandav25.RpkSASLEnvironmentVariables" (dict "a" (list $state)))) "r")))) -}}
{{- end -}}
{{- $notes = (concat (default (list) $notes) (list `` `Try some sample commands:`)) -}}
{{- if $anySASL -}}
{{- $notes = (concat (default (list) $notes) (list `Create a user:` `` (printf `  %s` (get (fromJson (include "redpandav25.RpkACLUserCreate" (dict "a" (list $state)))) "r")) `` `Give the user permissions:` `` (printf `  %s` (get (fromJson (include "redpandav25.RpkACLCreate" (dict "a" (list $state)))) "r")))) -}}
{{- end -}}
{{- $notes = (concat (default (list) $notes) (list `` `Get the api status:` `` (printf `  %s` (get (fromJson (include "redpandav25.RpkClusterInfo" (dict "a" (list $state)))) "r")) `` `Create a topic` `` (printf `  %s` (get (fromJson (include "redpandav25.RpkTopicCreate" (dict "a" (list $state)))) "r")) `` `Describe the topic:` `` (printf `  %s` (get (fromJson (include "redpandav25.RpkTopicDescribe" (dict "a" (list $state)))) "r")) `` `Delete the topic:` `` (printf `  %s` (get (fromJson (include "redpandav25.RpkTopicDelete" (dict "a" (list $state)))) "r")))) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $notes) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpandav25.RpkACLUserCreate" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (printf `rpk acl user create myuser --new-password changeme --mechanism %s` (get (fromJson (include "redpandav25.GetSASLMechanism" (dict "a" (list $state)))) "r"))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpandav25.GetSASLMechanism" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (ne (toJson $state.Values.auth.sasl) "null") -}}
{{- $_is_returning = true -}}
{{- (dict "r" $state.Values.auth.sasl.mechanism) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" "SCRAM-SHA-512") | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpandav25.RpkACLCreate" -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" `rpk acl create --allow-principal 'myuser' --allow-host '*' --operation all --topic 'test-topic'`) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpandav25.RpkClusterInfo" -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" `rpk cluster info`) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpandav25.RpkTopicCreate" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (printf `rpk topic create test-topic -p 3 -r %d` (min (3 | int64) (($state.Values.statefulset.replicas | int) | int64)))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpandav25.RpkTopicDescribe" -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" `rpk topic describe test-topic`) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpandav25.RpkTopicDelete" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" `rpk topic delete test-topic`) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpandav25.RpkSASLEnvironmentVariables" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (get (fromJson (include "redpandav25.RedpandaAtLeast_23_2_1" (dict "a" (list $state)))) "r") -}}
{{- $_is_returning = true -}}
{{- (dict "r" `RPK_USER RPK_PASS RPK_SASL_MECHANISM`) | toJson -}}
{{- break -}}
{{- else -}}
{{- $_is_returning = true -}}
{{- (dict "r" `REDPANDA_SASL_USERNAME REDPANDA_SASL_PASSWORD REDPANDA_SASL_MECHANISM`) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}
{{- end -}}

