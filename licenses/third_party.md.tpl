# Licenses list

<!--

This list can be auto generated with go-licenses

run `task generate:third-party-licenses-list`

-->

# Go deps _used_ in production in K8S (exclude all test dependencies)

| software     | license        |
| :----------: | :------------: |
{{ range . -}}
  {{- if eq .Name "buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate" -}}
| {{ .Name }} | [{{ .LicenseName }}](https://github.com/bufbuild/protovalidate/blob/main/LICENSE) |
  {{- else if eq .Name "buf.build/gen/go/grpc-ecosystem/grpc-gateway/protocolbuffers/go/protoc-gen-openapiv2/options" -}}
| {{ .Name }} | [{{ .LicenseName }}](https://github.com/grpc-ecosystem/grpc-gateway/blob/main/LICENSE) |
  {{- else if eq .Name "github.com/alibabacloud-go/cr-20160607/client" -}}
| {{ .Name }} | [Apache-2.0](https://github.com/alibabacloud-go/cr-20160607/blob/master/LICENSE) |
  {{- else -}}
| {{ .Name }} | [{{ .LicenseName }}]({{ .LicenseURL }}) |
  {{- end }}
{{ end }}
