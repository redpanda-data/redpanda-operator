@vcluster:k8s.example
Feature: V1 clusters on a non-default cluster domain
  # The operator's V1 client builders derive the broker FQDNs from
  # --cluster-domain, the same value the V1 controller mints node certificate
  # SANs from. This feature runs in a vcluster whose DNS serves "k8s.example"
  # and installs its own operator with the matching flag. Every V2 CR that
  # reaches a V1 Cluster over the admin API goes through those builders, so a
  # User CR reaching Synced proves the client dialed a resolvable hostname and
  # verified the node certificate over mTLS on the admin listener.
  @skip:gke @skip:aks @skip:eks
  Scenario: User CRs sync against a V1 cluster with a TLS admin API on a custom domain
    Given I helm install "redpanda-operator" "../operator/chart" with values:
    """
    image:
      tag: dev
      repository: localhost/redpanda-operator
    crds:
      enabled: true
      experimental: true
    vectorizedControllers:
      enabled: true
    additionalCmdFlags:
      - --cluster-domain=${CLUSTER_DOMAIN}
      - --configurator-image-pull-policy=IfNotPresent
    """
    And I apply Kubernetes manifest:
    """
    ---
    apiVersion: redpanda.vectorized.io/v1alpha1
    kind: Cluster
    metadata:
      name: tls-admin
    spec:
      image: ${DEFAULT_REDPANDA_REPO}
      version: ${DEFAULT_REDPANDA_TAG}
      replicas: 1
      enableSasl: true
      resources:
        requests:
          cpu: "100m"
          memory: 512Mi
        limits:
          cpu: "100m"
          memory: 512Mi
      configuration:
        rpcServer:
          port: 33145
        kafkaApi:
          - port: 9092
        adminApi:
          - port: 9644
            tls:
              enabled: true
              requireClientAuth: true
        schemaRegistryApi:
          - port: 8081
        developerMode: true
        additionalCommandlineArguments:
          dump-memory-diagnostics-on-alloc-failure-kind: all
          abort-on-seastar-bad-alloc: ''
    """
    And vectorized cluster "tls-admin" is available
    # The test-side Factory is the operator's own; it must reach the admin
    # API through the mTLS listener on the custom domain before the operator
    # is asked to.
    And cluster "tls-admin" admin API should show 1 brokers
    # The apply step takes one document at a time.
    When I apply Kubernetes manifest:
    """
    ---
    apiVersion: v1
    kind: Secret
    metadata:
      name: travis-password
    stringData:
      password: password
    """
    And I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: User
    metadata:
      name: travis
    spec:
      cluster:
        clusterRef:
          group: redpanda.vectorized.io
          kind: Cluster
          name: tls-admin
      authentication:
        type: scram-sha-512
        password:
          valueFrom:
            secretKeyRef:
              name: travis-password
              key: password
    """
    Then user "travis" is successfully synced
    And "travis" should be able to authenticate to the vectorized "tls-admin" cluster with password "password" and mechanism "SCRAM-SHA-512"
    # Role CRs are the sev-2 path: Cloud RBAC grants land as internal roles
    # over the admin API. The ACL goes through the Kafka client, so both V1
    # builders have to agree on the domain.
    When I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: RedpandaRole
    metadata:
      name: reader-role-k8s
    spec:
      cluster:
        clusterRef:
          group: redpanda.vectorized.io
          kind: Cluster
          name: tls-admin
      internal: true
      principals:
        - User:travis
      authorization:
        acls:
          - type: allow
            resource:
              type: topic
              name: internal-
              patternType: prefixed
            operations: [Read, Describe]
    """
    Then role "reader-role-k8s" is successfully synced
    And role "__reader-role-k8s" should exist in vectorized cluster "tls-admin" with effective name "__reader-role-k8s"
    And role "__reader-role-k8s" should have members "travis" in vectorized cluster "tls-admin" with effective name "__reader-role-k8s"
    And role "reader-role-k8s" should have ACLs for topic pattern "internal-" in vectorized cluster "tls-admin"
