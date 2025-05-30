# Redpanda Console Helm Chart Specification
---
description: Find the default values and descriptions of settings in the Redpanda Console Helm chart.
---

![Version: 3.1.0](https://img.shields.io/badge/Version-3.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: v3.1.0](https://img.shields.io/badge/AppVersion-v3.1.0-informational?style=flat-square)

This page describes the official Redpanda Console Helm Chart. In particular, this page describes the contents of the chart’s [`values.yaml` file](https://github.com/redpanda-data/helm-charts/blob/main/charts/console/values.yaml).
Each of the settings is listed and described on this page, along with any default values.

The Redpanda Console Helm chart is included as a subchart in the Redpanda Helm chart so that you can deploy and configure Redpanda and Redpanda Console together.
For instructions on how to install and use the chart, refer to the [deployment documentation](https://docs.redpanda.com/docs/deploy/deployment-option/self-hosted/kubernetes/kubernetes-deploy/).
For instructions on how to override and customize the chart’s values, see [Configure Redpanda Console](https://docs.redpanda.com/docs/manage/kubernetes/configure-helm-chart/#configure-redpanda-console).

----------------------------------------------
Autogenerated from chart metadata using [helm-docs v1.14.2](https://github.com/norwoodj/helm-docs/releases/v1.14.2)

## Source Code

* <https://github.com/redpanda-data/redpanda-operator/tree/main/charts/console>

## Requirements

Kubernetes: `>= 1.25.0-0`

## Settings

### [affinity](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=affinity)

**Default:** `{}`

### [annotations](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=annotations)

Annotations to add to the deployment.

**Default:** `{}`

### [automountServiceAccountToken](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=automountServiceAccountToken)

Automount API credentials for the Service Account into the pod. Console does not communicate with Kubernetes API.

**Default:** `false`

### [autoscaling.enabled](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=autoscaling.enabled)

**Default:** `false`

### [autoscaling.maxReplicas](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=autoscaling.maxReplicas)

**Default:** `100`

### [autoscaling.minReplicas](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=autoscaling.minReplicas)

**Default:** `1`

### [autoscaling.targetCPUUtilizationPercentage](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=autoscaling.targetCPUUtilizationPercentage)

**Default:** `80`

### [commonLabels](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=commonLabels)

**Default:** `{}`

### [config](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=config)

Settings for the `Config.yaml` (required). For a reference of configuration settings, see the [Redpanda Console documentation](https://docs.redpanda.com/docs/reference/console/config/).

**Default:** `{}`

### [configmap.create](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=configmap.create)

**Default:** `true`

### [deployment.create](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=deployment.create)

**Default:** `true`

### [extraContainers](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=extraContainers)

Add additional containers, such as for oauth2-proxy.

**Default:** `[]`

### [extraEnv](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=extraEnv)

Additional environment variables for the Redpanda Console Deployment.

**Default:** `[]`

### [extraEnvFrom](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=extraEnvFrom)

Additional environment variables for Redpanda Console mapped from Secret or ConfigMap.

**Default:** `[]`

### [extraVolumeMounts](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=extraVolumeMounts)

Add additional volume mounts, such as for TLS keys.

**Default:** `[]`

### [extraVolumes](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=extraVolumes)

Add additional volumes, such as for TLS keys.

**Default:** `[]`

### [fullnameOverride](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=fullnameOverride)

Override `console.fullname` template.

**Default:** `""`

### [image](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=image)

Redpanda Console Docker image settings.

**Default:**

```
{"pullPolicy":"IfNotPresent","registry":"docker.redpanda.com","repository":"redpandadata/console","tag":""}
```

### [image.pullPolicy](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=image.pullPolicy)

The imagePullPolicy.

**Default:** `"IfNotPresent"`

### [image.repository](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=image.repository)

Docker repository from which to pull the Redpanda Docker image.

**Default:** `"redpandadata/console"`

### [image.tag](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=image.tag)

The Redpanda Console version. See DockerHub for: [All stable versions](https://hub.docker.com/r/redpandadata/console/tags) and [all unstable versions](https://hub.docker.com/r/redpandadata/console-unstable/tags).

**Default:** `Chart.appVersion`

### [imagePullSecrets](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=imagePullSecrets)

Pull secrets may be used to provide credentials to image repositories See https://kubernetes.io/docs/tasks/configure-pod-container/pull-image-private-registry/

**Default:** `[]`

### [ingress.annotations](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=ingress.annotations)

**Default:** `{}`

### [ingress.enabled](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=ingress.enabled)

**Default:** `false`

### [ingress.hosts[0].host](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=ingress.hosts[0].host)

**Default:** `"chart-example.local"`

### [ingress.hosts[0].paths[0].path](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=ingress.hosts[0].paths[0].path)

**Default:** `"/"`

### [ingress.hosts[0].paths[0].pathType](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=ingress.hosts[0].paths[0].pathType)

**Default:** `"ImplementationSpecific"`

### [ingress.tls](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=ingress.tls)

**Default:** `[]`

### [initContainers](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=initContainers)

Any initContainers defined should be written here

**Default:** `{"extraInitContainers":""}`

### [initContainers.extraInitContainers](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=initContainers.extraInitContainers)

Additional set of init containers

**Default:** `""`

### [livenessProbe](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=livenessProbe)

Settings for liveness and readiness probes. For details, see the [Kubernetes documentation](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-probes/#configure-probes).

**Default:**

```
{"failureThreshold":3,"periodSeconds":10,"successThreshold":1,"timeoutSeconds":1}
```

### [nameOverride](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=nameOverride)

Override `console.name` template.

**Default:** `""`

### [nodeSelector](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=nodeSelector)

**Default:** `{}`

### [podAnnotations](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=podAnnotations)

**Default:** `{}`

### [podLabels](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=podLabels)

**Default:** `{}`

### [podSecurityContext.fsGroup](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=podSecurityContext.fsGroup)

**Default:** `99`

### [podSecurityContext.fsGroupChangePolicy](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=podSecurityContext.fsGroupChangePolicy)

**Default:** `"Always"`

### [podSecurityContext.runAsUser](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=podSecurityContext.runAsUser)

**Default:** `99`

### [priorityClassName](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=priorityClassName)

PriorityClassName given to Pods. For details, see the [Kubernetes documentation](https://kubernetes.io/docs/concepts/configuration/pod-priority-preemption/#priorityclass).

**Default:** `""`

### [readinessProbe.failureThreshold](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=readinessProbe.failureThreshold)

**Default:** `3`

### [readinessProbe.initialDelaySeconds](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=readinessProbe.initialDelaySeconds)

Grant time to test connectivity to upstream services such as Kafka and Schema Registry.

**Default:** `10`

### [readinessProbe.periodSeconds](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=readinessProbe.periodSeconds)

**Default:** `10`

### [readinessProbe.successThreshold](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=readinessProbe.successThreshold)

**Default:** `1`

### [readinessProbe.timeoutSeconds](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=readinessProbe.timeoutSeconds)

**Default:** `1`

### [replicaCount](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=replicaCount)

**Default:** `1`

### [resources](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=resources)

**Default:** `{}`

### [secret](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=secret)

Create a new Kubernetes Secret for all sensitive configuration inputs. Each provided Secret is mounted automatically and made available to the Pod. If you want to use one or more existing Secrets, you can use the `extraEnvFrom` list to mount environment variables from string and secretMounts to mount files such as Certificates from Secrets.

**Default:**

```
{"authentication":{"jwtSigningKey":"","oidc":{}},"create":true,"kafka":{},"license":"","redpanda":{"adminApi":{}},"schemaRegistry":{},"serde":{}}
```

### [secret.kafka](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=secret.kafka)

Kafka Secrets.

**Default:** `{}`

### [secretMounts](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=secretMounts)

SecretMounts is an abstraction to make a Secret available in the container's filesystem. Under the hood it creates a volume and a volume mount for the Redpanda Console container.

**Default:** `[]`

### [securityContext.runAsNonRoot](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=securityContext.runAsNonRoot)

**Default:** `true`

### [service.annotations](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=service.annotations)

Override the value in `console.config.server.listenPort` if not `nil` targetPort:

**Default:** `{}`

### [service.port](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=service.port)

**Default:** `8080`

### [service.type](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=service.type)

**Default:** `"ClusterIP"`

### [serviceAccount.annotations](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=serviceAccount.annotations)

Annotations to add to the service account.

**Default:** `{}`

### [serviceAccount.automountServiceAccountToken](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=serviceAccount.automountServiceAccountToken)

Specifies whether a service account should automount API-Credentials. Console does not communicate with Kubernetes API. The ServiceAccount could be used for workload identity.

**Default:** `false`

### [serviceAccount.create](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=serviceAccount.create)

Specifies whether a service account should be created.

**Default:** `true`

### [serviceAccount.name](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=serviceAccount.name)

The name of the service account to use. If not set and `serviceAccount.create` is `true`, a name is generated using the `console.fullname` template

**Default:** `""`

### [strategy](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=strategy)

**Default:** `{}`

### [tests.enabled](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=tests.enabled)

**Default:** `true`

### [tolerations](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=tolerations)

**Default:** `[]`

### [topologySpreadConstraints](https://artifacthub.io/packages/helm/redpanda-data/console?modal=values&path=topologySpreadConstraints)

**Default:** `[]`

