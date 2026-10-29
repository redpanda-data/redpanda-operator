# Pipeline API (prototype)

Serves Redpanda Connect's `redpanda.api.dataplane.v1.PipelineService` from
`Pipeline` resources in one namespace. It's the self-hosted counterpart of
Redpanda Cloud's `redpanda-connect-api`: Redpanda Console proxies its pipeline
calls here and never talks to the Kubernetes API itself.

```
Browser ──► Console (enterprise) ──gRPC──► pipeline-api ──► Kubernetes API ──► Pipeline controller
            cloud.redpandaConnect        TokenReview,      Pipeline CRs
            .address                     safe-subset
                                         writes
```

- Run with `redpanda-operator pipeline-api` (same image as the operator).
- It writes only `displayName`, `description`, `tags`, `configYaml`,
  `resources.requests`, `paused`, and the configured `cluster.clusterRef` and
  `userRef`. Callers never pick the cluster or the user.
- It only changes Pipelines labeled `console.redpanda.com/managed-by: console`
  whose stored spec has nothing outside those fields. Pipelines from kubectl
  or GitOps are listed read-only.
- `configYaml` is checked against a denylist (`command`, `subprocess`, `file`,
  `socket_server`, Bloblang `env()`/`file()`, `${REDPANDA_*}`/`${RPK_*}`).
- Callers authenticate with a bearer token, checked with TokenReview for the
  `redpanda-pipeline-api` audience, and must be in `--allowed-caller`.
  `X-Redpanda-Principal` (the Console user) is recorded for audit and
  `console.redpanda.com/created-by` only after that.

## Deploying

```yaml
apiVersion: v1
kind: ServiceAccount
metadata: {name: redpanda-pipeline-api, namespace: redpanda}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata: {name: redpanda-pipeline-api, namespace: redpanda}
rules:
- apiGroups: [cluster.redpanda.com]
  resources: [pipelines]
  verbs: [get, list, create, update, delete]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata: {name: redpanda-pipeline-api, namespace: redpanda}
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: Role, name: redpanda-pipeline-api}
subjects: [{kind: ServiceAccount, name: redpanda-pipeline-api, namespace: redpanda}]
---
# TokenReview, to authenticate Console.
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata: {name: redpanda-pipeline-api-auth-delegator}
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: ClusterRole, name: system:auth-delegator}
subjects: [{kind: ServiceAccount, name: redpanda-pipeline-api, namespace: redpanda}]
---
apiVersion: apps/v1
kind: Deployment
metadata: {name: redpanda-pipeline-api, namespace: redpanda}
spec:
  selector: {matchLabels: {app: redpanda-pipeline-api}}
  template:
    metadata: {labels: {app: redpanda-pipeline-api}}
    spec:
      serviceAccountName: redpanda-pipeline-api
      containers:
      - name: pipeline-api
        image: docker.redpanda.com/redpandadata/redpanda-operator:<operator version>
        # The image's default entrypoint runs the controller manager.
        command: [/redpanda-operator]
        args:
        - pipeline-api
        - --namespace=redpanda
        - --cluster-ref=redpanda
        - --user-ref=console-pipelines
        - --allowed-caller=system:serviceaccount:redpanda:redpanda-console
        ports: [{name: http, containerPort: 8080}]
        readinessProbe: {httpGet: {path: /healthz, port: http}}
---
apiVersion: v1
kind: Service
metadata: {name: redpanda-pipeline-api, namespace: redpanda}
spec:
  selector: {app: redpanda-pipeline-api}
  ports: [{name: http, port: 8080, targetPort: http}]
```

Console config:

```yaml
cloud:
  redpandaConnect:
    enabled: true
    address: redpanda-pipeline-api.redpanda.svc.cluster.local:8080
```

The Console CR and chart should render all of this and set the address
(not done yet).

## Open items

- **Console enterprise**: its proxy (`pkg/api/connect/pipeline/v1`) forwards
  the incoming request headers as is. It needs to send its own projected
  ServiceAccount token (audience `redpanda-pipeline-api`) as `Authorization`,
  send the logged-in user as `X-Redpanda-Principal`, and stop forwarding
  cookies. TLS: wire `CreateSecurePipelineAPIClient` and `--tls-cert-file`.
- **ListComponents, LintPipelineConfig, GetPipelineServiceConfigSchema**
  return Unimplemented. Console bundles the Connect schema, so it should serve
  these locally rather than proxying them.
- **Proto pin**: the BSR dataplane module the operator links predates `created_by`,
  `create_time`, `update_time`, the `states` list filter and `draft`. Bump it
  to populate them; the fields are wire-compatible either way.
- **Per-user Kubernetes RBAC** (the old `impersonateUser`): add a
  SubjectAccessReview for the principal if admins want Kubernetes, not
  Console, to decide who can edit pipelines.
