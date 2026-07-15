# Using the Operator with the Pipeline CRD

The `Pipeline` CRD (`cluster.redpanda.com/v1alpha2`, shortName `rpcn`) runs a
[Redpanda Connect](https://docs.redpanda.com/redpanda-connect/) pipeline as a
Kubernetes Deployment. You hand the operator a Connect config; it renders a
ConfigMap + Deployment, lints the config in an init container before the
pipeline starts, and reports health back on `.status`.

This guide is a step-by-step walkthrough: install the CRD, run the controller,
deploy a first pipeline, then bind it to a real Redpanda cluster with
credentials and secrets.

---

## Prerequisites

- A Kubernetes cluster (1.25+) and `kubectl` pointed at it.
- An **enterprise license** with the Connect product enabled. Connect is gated:
  without a valid license the controller reconciles Pipelines to
  `Ready=False` with a license reason (see [Step 2](#step-2-run-the-pipeline-controller)).
- The operator's container can pull the Connect image
  (`docker.redpanda.com/redpandadata/connect:4.96.0` by default).

---

## Step 1 — Install the Pipeline CRD

```bash
kubectl apply -f operator/config/crd/bases/cluster.redpanda.com_pipelines.yaml
kubectl wait --for=condition=Established crd/pipelines.cluster.redpanda.com --timeout=60s
```

If you intend to use `cluster.clusterRef` (Step 4), also install the `Redpanda`
and `User` CRDs — the controller watches the parent `Redpanda` CR, so its
informer needs that CRD present even before you create a cluster.

---

## Step 2 — Run the Pipeline controller

The Pipeline controller is registered into a controller-runtime manager by the
host operator binary. Two ways to run it:

- **Shipped operator:** the operator binary registers the Pipeline controller
  alongside the others; point it at the cluster and give it the license. The
  steps below apply unchanged.
- **This repo (library):** `operator-enterprise` is a library extraction and has
  no `cmd/run` entrypoint yet. Use the standalone **runner harness** at
  [`operator-connect-ent/runner`](https://github.com/david-yu/redpanda-setup/tree/main/operator-connect-ent)
  in `david-yu/redpanda-setup`, which wires *only* the Pipeline controller into
  a manager and runs out-of-cluster against your kubeconfig.

The controller reads the license from the path in `REDPANDA_LICENSE_FILE`:

```bash
export REDPANDA_LICENSE_FILE=/path/to/redpanda.license
# shipped operator: pass via its license flag/secret as documented for the operator
# runner harness: REDPANDA_LICENSE_FILE=... ./runner
```

> **License gate behaviour.** With no license, a Pipeline reconciles but stays
> `Ready=False` with a license reason — the CRD installs, the controller
> reconciles, status flows, but the pipeline never reaches `Running`. This is
> the intended enterprise gate, not a bug.

---

## Step 3 — Your first pipeline (inline config)

A minimal self-contained pipeline (`generate → stdout`), no cluster needed:

```yaml
apiVersion: cluster.redpanda.com/v1alpha2
kind: Pipeline
metadata:
  name: hello
  namespace: default
spec:
  configYaml: |
    input:
      generate:
        interval: 1s
        mapping: 'root.msg = "hello from connect"'
    output:
      stdout: {}
```

```bash
kubectl apply -f hello-pipeline.yaml

# watch it come up
kubectl get pipeline hello -w
# NAME    READY   PHASE     REPLICAS   AVAILABLE   AGE
# hello   True    Running   1          1           20s

# see the output
kubectl logs deploy/hello -c connect -f
```

What the operator did:

1. Rendered a ConfigMap (`hello`) holding `connect.yaml`.
2. Rendered a Deployment with a **`lint` init container** that runs
   `redpanda-connect lint /config/connect.yaml` — the pipeline pod only starts
   if the config passes lint. A bad config surfaces as `ConfigValid=False`.
3. Stamped the pod template with `cluster.redpanda.com/config-checksum` so any
   later `configYaml` change rolls the Deployment automatically.

Pause it (scales to zero, `phase=Stopped`) without deleting:

```bash
kubectl patch pipeline hello --type merge -p '{"spec":{"paused":true}}'
```

---

## Step 4 — Connect to a Redpanda cluster

Real pipelines read from / write to a Redpanda cluster. Bind the pipeline to a
`Redpanda` CR with `cluster.clusterRef`, and give it SASL credentials with a
`User` CR + `userRef`. The operator resolves the broker list, TLS, and SASL
from the referenced cluster and injects a top-level `redpanda:` shared-client
block into the rendered config — so `redpanda`, `redpanda_common`, and any
shared-client component work without inline credentials.

```yaml
# 1. a SASL user for the pipeline (its password lives in a Secret)
apiVersion: cluster.redpanda.com/v1alpha2
kind: User
metadata:
  name: pipeline-svc
  namespace: redpanda
spec:
  cluster:
    clusterRef:
      name: redpanda
  authentication:
    type: scram-sha-512
    password:
      valueFrom:
        secretKeyRef:
          name: pipeline-svc-password
          key: password
---
# 2. the pipeline, bound to the cluster + user
apiVersion: cluster.redpanda.com/v1alpha2
kind: Pipeline
metadata:
  name: ingest
  namespace: redpanda
spec:
  cluster:
    clusterRef:
      name: redpanda          # a Redpanda CR in this namespace
  userRef:
    name: pipeline-svc        # the User CR above
  configYaml: |
    input:
      generate:
        interval: 1s
        mapping: 'root.id = uuid_v4()'
    output:
      redpanda_common:
        topic: ingest-demo
```

```bash
kubectl apply -f ingest-pipeline.yaml
kubectl get pipeline ingest -o wide
```

Relevant conditions on `.status` reflect resolution: `ClusterRef`, `UserRef`
(see [Status reference](#status-reference)).

> When the pipeline has no `userRef`, the operator falls back to the SASL
> identity resolved from the cluster source (the cluster's bootstrap user for
> `clusterRef`). Prefer a dedicated `User` + `userRef` in production so each
> pipeline authenticates with ACL-scoped credentials.

### Connecting with `staticConfiguration`

Instead of `clusterRef`, hard-code the connection with
`cluster.staticConfiguration` — useful when the target cluster isn't managed
by a `Redpanda` CR in this namespace (external, self-managed, or Cloud).
`userRef` is mutually exclusive with `staticConfiguration`; SASL credentials
are declared inline:

```yaml
spec:
  cluster:
    staticConfiguration:
      kafka:
        brokers:
          - seed-0.example.com:9093
        tls:
          caCert:                     # omit for publicly-issued certs
            secretKeyRef:
              name: external-ca
              key: ca.crt
        sasl:
          username: pipeline-svc
          mechanism: SCRAM-SHA-512    # PLAIN, SCRAM-SHA-256, or SCRAM-SHA-512
          password:
            secretKeyRef:
              name: pipeline-svc-password
              key: password
  configYaml: |
    ...
```

The CA certificate may come from a `secretKeyRef` or `configMapKeyRef`; the
SASL password additionally supports `inline` (not recommended outside dev).

---

## Step 5 — Inject secrets with `valueSources`

Reference secrets in the config via `${NAME}` interpolation; each value is
pulled once from inline / ConfigMap / Secret and projected as an env var. This
avoids splatting a whole Secret into the pod env.

```yaml
spec:
  valueSources:
    - name: S3_SECRET_KEY
      source:
        secretKeyRef:
          name: s3-creds
          key: secret_access_key
  configYaml: |
    output:
      aws_s3:
        bucket: my-bucket
        credentials:
          secret: ${S3_SECRET_KEY}
```

Resolution is reported by the `ValueSourcesResolved` condition.

---

## Step 6 — Run setup steps with `extraInitContainers`

Need certs fetched, a cache warmed, or a dependency waited on before the
pipeline starts? `extraInitContainers` are run to completion, **in order, ahead
of the built-in `lint` container**, so anything they stage into a shared volume
is visible to lint and to the connect runtime.

```yaml
spec:
  extraInitContainers:
    - name: fetch-certs
      image: curlimages/curl:8.11.0
      command: ["sh", "-c", "curl -fsSL $CERT_URL -o /shared/ca.pem"]
      volumeMounts:
        - name: shared
          mountPath: /shared
  configYaml: |
    # ... pipeline that reads /shared/ca.pem ...
```

This is an unconstrained container passthrough (the pod's service account and
security posture apply). It is **not** the mechanism for a long-lived Connect
plugin sidecar — that is a separate, policy-constrained field tracked under the
RPCN custom-plugins RFC.

---

## Step 7 — Common knobs

```yaml
spec:
  replicas: 3                              # default 1; 0 to stop
  paused: true                             # scale to zero, keep the resource
  image: docker.redpanda.com/redpandadata/connect:4.96.0   # override the default
  resources:                               # standard pod resource requirements
    requests: { cpu: 100m, memory: 256Mi }
    limits:   { cpu: "1",  memory: 1Gi }
  serviceAccountName: my-pipeline-sa       # pod identity (IRSA / Workload Identity)
  nodeSelector: { disktype: ssd }
  zones: ["us-east-2a", "us-east-2b"]      # spread pods across AZs
  budget: { maxUnavailable: 1 }            # creates a PodDisruptionBudget
```

Image precedence: `spec.image` > operator chart default > the binary-baked
`connect:4.96.0`.

### Pinning a pipeline to a Kubernetes node pool

To make a pipeline run only on a specific node pool (e.g. a Connect-dedicated
EKS managed node group, isolated from the broker nodes), constrain its pods to
that pool's nodes:

```yaml
spec:
  # simplest: match the node pool's label. EKS managed node groups carry
  # eks.amazonaws.com/nodegroup=<name>; or label the pool yourself.
  nodeSelector:
    redpanda.com/node-pool: connect
  # if the pool is tainted (to keep other workloads off it), tolerate it:
  tolerations:
    - key: dedicated
      operator: Equal
      value: connect
      effect: NoSchedule
```

For scheduling that `nodeSelector` can't express — *any of several* acceptable
pools, a *preferred* (soft) pool, or pod anti-affinity to spread a pipeline —
use `spec.affinity` (a standard Kubernetes `Affinity`, merged with the
zone affinity from `Zones`):

```yaml
spec:
  affinity:
    nodeAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        nodeSelectorTerms:
          - matchExpressions:
              - key: redpanda.com/node-pool
                operator: In
                values: ["connect", "connect-spot"]   # either pool is fine
```

---

## Lifecycle and operational semantics

**Pipeline pods do not depend on the operator process.** The Deployment (and
its pods) are owned by the *Pipeline CR*, not by the operator, so an operator
SIGTERM, crash, restart, or upgrade never interrupts running pipelines — they
keep processing data unmanaged until the controller comes back and resumes
reconciling.

**Failures never tear down a running workload.** If the referenced `Redpanda`
CR, `User` CR, or enterprise license becomes unresolvable — transiently or
otherwise — the controller reports it on `.status` and stops *syncing* the
pipeline, but leaves the last-known-good Deployment running. The only thing
that deletes a pipeline's Deployment is deleting the Pipeline CR itself.

**License semantics.** A valid Connect-entitled enterprise license is required
to *create or update* pipeline workloads. When the license is missing, expired,
or unreadable, new Pipelines report `Ready=False` / `LicenseInvalid` and get no
workload; existing workloads keep running (the Connect runtime enforces its own
license gate for enterprise components whenever pods restart) but stop
receiving spec updates until the license is fixed.

**Disabling the Connect controller.** If you no longer use Connect pipelines,
disable the controller by setting `Disabled: true` on `pipeline.Controller`
(exposed by the operator runner as `--enable-connect-controller=false` /
`connectController.enabled: false` once the cmd wiring lands). Disabling
registers no watches and no telemetry. Note:

- Existing pipeline Deployments/pods keep running unmanaged (see above).
  Delete the Pipeline CRs *before* disabling if you want the workloads gone.
- Pipeline CRs carry the `operator.redpanda.com/finalizer`; deleting one while
  the controller is disabled leaves it `Terminating` until the controller is
  re-enabled or the finalizer is removed manually
  (`kubectl patch pipeline <name> -p '{"metadata":{"finalizers":null}}' --type=merge`).
- The Pipeline CRD can stay installed; CRs become inert.

---

## Status reference

```bash
kubectl get pipeline <name> -o yaml | yq '.status'
```

**Phases** (`.status.phase`):

| Phase | Meaning |
|---|---|
| `Pending` | accepted, not yet acted on |
| `Provisioning` | Deployment created, pods not yet ready |
| `Running` | desired replicas ready |
| `Stopped` | `paused: true` (or `replicas: 0`) |
| `Unknown` | state could not be determined |

**Conditions** (`.status.conditions[]`):

| Type | True means |
|---|---|
| `Ready` | the pipeline is healthy and running (the headline condition) |
| `ConfigValid` | the Connect config passed lint |
| `ClusterRef` | the referenced `Redpanda` cluster was resolved |
| `UserRef` | the referenced `User`'s credentials were resolved |
| `ValueSourcesResolved` | every `valueSources` entry was fetched |

---

## Troubleshooting

| Symptom | Cause / fix |
|---|---|
| `Ready=False`, reason mentions license | No / invalid Connect license. Set `REDPANDA_LICENSE_FILE` (Step 2). |
| `ConfigValid=False` | The `lint` init container rejected the config. `kubectl logs <pod> -c lint` shows the lint error. |
| Pod stuck `Init:` / `ImagePullBackOff` | Operator/pod can't pull the Connect image, or an `extraInitContainers` image. Check image ref + pull secrets. |
| `redpanda_common` errors with `shared client not found` | Bind the pipeline to a cluster (`clusterRef` + `userRef`, Step 4) so the operator emits the top-level `redpanda:` shared-client block. |
| Changed `configYaml` but pods didn't restart | They should — the operator stamps a `cluster.redpanda.com/config-checksum` annotation that rolls the Deployment. Confirm the new spec was applied (`kubectl get pipeline <name> -o yaml`). |
| `ClusterRef=False` | The named `Redpanda` CR doesn't exist in the Pipeline's namespace, or its CRD isn't installed. |
