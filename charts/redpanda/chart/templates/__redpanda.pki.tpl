{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/pki.go" */ -}}

{{- define "_redpanda.PKI.ServerKeypair" -}}
{{- $p := (index .a 0) -}}
{{- $name := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (ternary (index $p.Certificates $name) (dict "Server" (dict "Name" "" "Secret" (dict) "Request" (coalesce nil) "CA" (coalesce nil)) "Client" (coalesce nil)) (hasKey $p.Certificates $name)).Server) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.PKI.ClientKeypair" -}}
{{- $p := (index .a 0) -}}
{{- $name := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_57_cert_1_ok_2 := (get (fromJson (include "_shims.dicttest" (dict "a" (list $p.Certificates $name (dict "Server" (dict "Name" "" "Secret" (dict) "Request" (coalesce nil) "CA" (coalesce nil)) "Client" (coalesce nil)))))) "r") -}}
{{- $cert_1 := (index $_57_cert_1_ok_2 0) -}}
{{- $ok_2 := (index $_57_cert_1_ok_2 1) -}}
{{- if $ok_2 -}}
{{- $_is_returning = true -}}
{{- (dict "r" $cert_1.Client) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.PKI.names" -}}
{{- $p := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "_shims.slices_Sorted" (dict "a" (list (keys $p.Certificates))))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.PKI.Render" -}}
{{- $p := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $certs := (coalesce nil) -}}
{{- range $_, $name := (get (fromJson (include "_redpanda.PKI.names" (dict "a" (list $p)))) "r") -}}
{{- $cert := (ternary (index $p.Certificates $name) (dict "Server" (dict "Name" "" "Secret" (dict) "Request" (coalesce nil) "CA" (coalesce nil)) "Client" (coalesce nil)) (hasKey $p.Certificates $name)) -}}
{{- if (ne (toJson $cert.Server.Request) "null") -}}
{{- $certs = (concat (default (list) $certs) (list (get (fromJson (include "_redpanda.PKI.serverCertificate" (dict "a" (list $p $cert.Server)))) "r"))) -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $name := (get (fromJson (include "_redpanda.PKI.names" (dict "a" (list $p)))) "r") -}}
{{- $cert := (ternary (index $p.Certificates $name) (dict "Server" (dict "Name" "" "Secret" (dict) "Request" (coalesce nil) "CA" (coalesce nil)) "Client" (coalesce nil)) (hasKey $p.Certificates $name)) -}}
{{- if (and (ne (toJson $cert.Client) "null") (ne (toJson $cert.Client.Request) "null")) -}}
{{- $certs = (concat (default (list) $certs) (list (get (fromJson (include "_redpanda.PKI.clientCertificate" (dict "a" (list $p $cert.Client)))) "r"))) -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $certs) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.PKI.Mounts" -}}
{{- $p := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $mounts := (coalesce nil) -}}
{{- range $_, $name := (get (fromJson (include "_redpanda.PKI.names" (dict "a" (list $p)))) "r") -}}
{{- $cert := (ternary (index $p.Certificates $name) (dict "Server" (dict "Name" "" "Secret" (dict) "Request" (coalesce nil) "CA" (coalesce nil)) "Client" (coalesce nil)) (hasKey $p.Certificates $name)) -}}
{{- $mounts = (concat (default (list) $mounts) (list (get (fromJson (include "_redpanda.Keypair.Mount" (dict "a" (list $cert.Server)))) "r"))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $name := (get (fromJson (include "_redpanda.PKI.names" (dict "a" (list $p)))) "r") -}}
{{- $cert := (ternary (index $p.Certificates $name) (dict "Server" (dict "Name" "" "Secret" (dict) "Request" (coalesce nil) "CA" (coalesce nil)) "Client" (coalesce nil)) (hasKey $p.Certificates $name)) -}}
{{- if (eq (toJson $cert.Client) "null") -}}
{{- continue -}}
{{- end -}}
{{- $mounts = (concat (default (list) $mounts) (list (get (fromJson (include "_redpanda.Keypair.Mount" (dict "a" (list $cert.Client)))) "r"))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $mounts) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.PKI.Volumes" -}}
{{- $p := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $volumes := (coalesce nil) -}}
{{- range $_, $name := (get (fromJson (include "_redpanda.PKI.names" (dict "a" (list $p)))) "r") -}}
{{- $cert := (ternary (index $p.Certificates $name) (dict "Server" (dict "Name" "" "Secret" (dict) "Request" (coalesce nil) "CA" (coalesce nil)) "Client" (coalesce nil)) (hasKey $p.Certificates $name)) -}}
{{- $volumes = (concat (default (list) $volumes) (list (get (fromJson (include "_redpanda.Keypair.Volume" (dict "a" (list $cert.Server)))) "r"))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $name := (get (fromJson (include "_redpanda.PKI.names" (dict "a" (list $p)))) "r") -}}
{{- $cert := (ternary (index $p.Certificates $name) (dict "Server" (dict "Name" "" "Secret" (dict) "Request" (coalesce nil) "CA" (coalesce nil)) "Client" (coalesce nil)) (hasKey $p.Certificates $name)) -}}
{{- if (eq (toJson $cert.Client) "null") -}}
{{- continue -}}
{{- end -}}
{{- $volumes = (concat (default (list) $volumes) (list (get (fromJson (include "_redpanda.Keypair.Volume" (dict "a" (list $cert.Client)))) "r"))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $volumes) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.PKI.serverCertificate" -}}
{{- $p := (index .a 0) -}}
{{- $k := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict) "spec" (dict "secretName" "" "issuerRef" (dict "name" "")) "status" (dict)) (mustMergeOverwrite (dict) (dict "apiVersion" "cert-manager.io/v1" "kind" "Certificate")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" $k.Request.objectName "namespace" $p.Namespace "labels" $p.Labels "annotations" $p.Annotations)) "spec" (mustMergeOverwrite (dict "secretName" "" "issuerRef" (dict "name" "")) (dict "dnsNames" $k.Request.dnsNames "duration" $k.Request.duration "isCA" false "issuerRef" $k.Request.issuerRef "secretName" $k.Secret.name "privateKey" (mustMergeOverwrite (dict) (dict "algorithm" "ECDSA" "size" (256 | int)))))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.PKI.clientCertificate" -}}
{{- $p := (index .a 0) -}}
{{- $k := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict) "spec" (dict "secretName" "" "issuerRef" (dict "name" "")) "status" (dict)) (mustMergeOverwrite (dict) (dict "apiVersion" "cert-manager.io/v1" "kind" "Certificate")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" $k.Request.objectName "namespace" $p.Namespace "labels" $p.Labels "annotations" $p.Annotations)) "spec" (mustMergeOverwrite (dict "secretName" "" "issuerRef" (dict "name" "")) (dict "commonName" $k.Request.commonName "duration" $k.Request.duration "isCA" false "secretName" $k.Secret.name "privateKey" (mustMergeOverwrite (dict) (dict "algorithm" "ECDSA" "size" (256 | int))) "issuerRef" $k.Request.issuerRef))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.Keypair.MountPath" -}}
{{- $k := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (printf "%s/%s" "/etc/tls/certs" $k.Name)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.Keypair.VolumeName" -}}
{{- $k := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (printf "redpanda-%s-cert" $k.Name)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.Keypair.CertFile" -}}
{{- $k := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (printf "%s/%s" (get (fromJson (include "_redpanda.Keypair.MountPath" (dict "a" (list $k)))) "r") "tls.crt")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.Keypair.KeyFile" -}}
{{- $k := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (printf "%s/%s" (get (fromJson (include "_redpanda.Keypair.MountPath" (dict "a" (list $k)))) "r") "tls.key")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.Keypair.CAFile" -}}
{{- $k := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (eq (toJson $k.CA) "null") -}}
{{- $_is_returning = true -}}
{{- (dict "r" "") | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (printf "%s/%s" (get (fromJson (include "_redpanda.Keypair.MountPath" (dict "a" (list $k)))) "r") $k.CA)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.Keypair.CAOrCertFile" -}}
{{- $k := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (ne (toJson $k.CA) "null") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "_redpanda.Keypair.CAFile" (dict "a" (list $k)))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "_redpanda.Keypair.CertFile" (dict "a" (list $k)))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.Keypair.Volume" -}}
{{- $k := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "name" "") (mustMergeOverwrite (dict) (dict "secret" (mustMergeOverwrite (dict) (dict "secretName" $k.Secret.name "defaultMode" (288 | int))))) (dict "name" (get (fromJson (include "_redpanda.Keypair.VolumeName" (dict "a" (list $k)))) "r")))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.Keypair.Mount" -}}
{{- $k := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" (get (fromJson (include "_redpanda.Keypair.VolumeName" (dict "a" (list $k)))) "r") "mountPath" (get (fromJson (include "_redpanda.Keypair.MountPath" (dict "a" (list $k)))) "r")))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.ServiceSANs" -}}
{{- $fullname := (index .a 0) -}}
{{- $service := (index .a 1) -}}
{{- $namespace := (index .a 2) -}}
{{- $clusterDomain := (index .a 3) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $bootstrap := (printf "%s-cluster.%s.%s" $fullname $service $namespace) -}}
{{- $svc := (printf "%s.%s" $service $namespace) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list (printf "%s.svc.%s" $bootstrap $clusterDomain) (printf "%s.svc" $bootstrap) $bootstrap (printf "*.%s.svc.%s" $bootstrap $clusterDomain) (printf "*.%s.svc" $bootstrap) (printf "*.%s" $bootstrap) (printf "%s.svc.%s" $svc $clusterDomain) (printf "%s.svc" $svc) $svc (printf "*.%s.svc.%s" $svc $clusterDomain) (printf "*.%s.svc" $svc) (printf "*.%s" $svc))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.NamespaceSANs" -}}
{{- $namespace := (index .a 0) -}}
{{- $clusterDomain := (index .a 1) -}}
{{- $brokers := (index .a 2) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $names := (list (printf "*.%s.svc.%s" $namespace $clusterDomain) (printf "*.%s.svc" $namespace)) -}}
{{- range $_, $broker := $brokers -}}
{{- $names = (concat (default (list) $names) (list (printf "%s.%s" $broker $namespace))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $names) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.ClusterSetSANs" -}}
{{- $fullname := (index .a 0) -}}
{{- $service := (index .a 1) -}}
{{- $namespace := (index .a 2) -}}
{{- $brokers := (index .a 3) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $names := (list (printf "%s-cluster.%s.%s.svc.clusterset.local" $fullname $service $namespace) (printf "*.%s-cluster.%s.%s.svc.clusterset.local" $fullname $service $namespace) (printf "%s.%s.svc.clusterset.local" $service $namespace) (printf "*.%s.%s.svc.clusterset.local" $service $namespace)) -}}
{{- range $_, $broker := $brokers -}}
{{- $names = (concat (default (list) $names) (list (printf "%s.%s.svc.clusterset.local" $broker $namespace))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $names) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.DomainSANs" -}}
{{- $domain := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (eq $domain "") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list $domain (printf "*.%s" $domain))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

