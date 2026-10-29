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

The Console resource and the Console Helm chart deploy it with `pipelines`:

```yaml
apiVersion: cluster.redpanda.com/v1alpha2
kind: Console
spec:
  cluster: {clusterRef: {name: redpanda}}
  pipelines:
    enabled: true
    clusterRef: redpanda        # pinned on every Console pipeline
    userRef: console-pipelines  # User every Console pipeline runs as; required with clusterRef
```

That renders, in Console's namespace:

- a ServiceAccount, Role (Pipelines get, list, create, update, delete),
  RoleBinding, Deployment and Service named `<fullname>-pipeline-api`;
- a ClusterRoleBinding to `system:auth-delegator`, for TokenReview;
- a NetworkPolicy that admits only Console's Pods (`networkPolicy.enabled`);
- with `tls.enabled` (the default) and no `tls.secretName`, cert-manager
  Issuers and Certificates for a self-signed CA (five years) and the API's
  serving certificate. The API reloads renewed certificates; Console reads
  the CA at startup.

The Console Pods get a projected ServiceAccount token for the
`redpanda-pipeline-api` audience and the CA, and Console's config gets:

```yaml
cloud:
  redpandaConnect:
    enabled: true
    address: <fullname>-pipeline-api.<namespace>.svc:8080
    tokenFile: /var/run/secrets/redpanda.com/pipeline-api/token
    tls:
      enabled: true
      caFilepath: /etc/console/pipeline-api-ca/ca.crt
```

Console rejects unknown config keys at startup, so versions that predate
`tokenFile` and `tls` (console-enterprise#844) don't start with this config.
For those, set `allowUnauthenticated: true` and `tls.enabled: false`: the
chart then renders only `enabled` and `address`, runs the API with
`--allow-unauthenticated`, and leaves the NetworkPolicy as the only guard,
which needs a CNI that enforces NetworkPolicies.

The operator runs it from its own image and needs `connectController.enabled`
for the RBAC to create the Role. Helm installs of the Console chart set
`pipelines.image.tag` to an operator version.

## Open items

- **Console enterprise**: `cloud.redpandaConnect.tokenFile` and `.tls` are in
  console-enterprise#844, which also serves `ListComponents` and
  `GetPipelineServiceConfigSchema` from Console's bundled Connect schema when
  this API returns Unimplemented. The editor's components then match
  Console's bundled Connect version, not the pipelines' image.
- **LintPipelineConfig** returns Unimplemented, and the editor shows no
  server-side lint hints.
- **Proto pin**: the BSR dataplane module the operator links predates `created_by`,
  `create_time`, `update_time`, the `states` list filter and `draft`. Bump it
  to populate them; the fields are wire-compatible either way.
- **Per-user Kubernetes RBAC** (the old `impersonateUser`): add a
  SubjectAccessReview for the principal if admins want Kubernetes, not
  Console, to decide who can edit pipelines.
