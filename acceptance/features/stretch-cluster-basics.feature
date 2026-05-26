@multicluster @dev-env
Feature: Multicluster Operator

  @skip:gke @skip:aks @skip:eks
  Scenario: Multicluster finalizers
    Given I create a multicluster operator named "multicluster" with 3 nodes
    And I apply a multicluster Kubernetes manifest to "multicluster":
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: StretchCluster
    metadata:
      name: cluster
      namespace: default
    spec:
      # Cluster-wide PVC annotations. Pools inherit these by default; one
      # region overrides below to exercise the per-key merge.
      storage:
        persistentVolume:
          enabled: true
          annotations:
            team: platform
            env: prod
    """
    Then in "multicluster" the Kubernetes object "cluster" in namespace "default" of type "StretchCluster.v1alpha2.cluster.redpanda.com" should have finalizer "operator.redpanda.com/finalizer"
    # All three RedpandaBrokerPools share a base spec; vc-0 also gets a
    # storage override applied at creation time. The base spec inherits
    # spec.storage.persistentVolume.annotations from StretchClusterSpec
    # (team=platform, env=prod). The override on vc-0 turns this into
    # per-key merge: env is bumped to staging and owner is added. vc-1
    # and vc-2 keep the cluster-inherited values unchanged.
    #
    # Storage overrides must be set at creation time — volumeClaimTemplates
    # on the rendered StatefulSet is immutable, so a post-create patch
    # would be rejected by the API server.
    And I apply a RedpandaBrokerPool Kubernetes manifest to "multicluster" with extra spec for region "vc-0":
    """
    spec:
      external:
        enabled: false
      rbac:
        enabled: true
        rpkDebugBundle: true
      tls:
        enabled: true
        certs:
          issuer-managed:
            caEnabled: true
            applyInternalDNSNames: true
            issuerRef:
              name: custom-issuer-managed-issuer
              kind: Issuer
              group: cert-manager.io
          user-provided:
            caEnabled: true
            secretRef:
              name: cluster-user-provided-cert
      listeners:
        admin:
          tls:
            cert: issuer-managed
        kafka:
          tls:
            cert: user-provided
        http:
          tls:
            cert: issuer-managed
        schemaRegistry:
          tls:
            cert: issuer-managed
        rpc:
          tls:
            cert: issuer-managed
      clusterRef:
        group: cluster.redpanda.com
        kind: StretchCluster
        name: cluster
      replicas: 1
      image:
        repository: redpandadata/redpanda
        tag: v25.2.1
      sidecarImage:
        repository: localhost/redpanda-operator
        tag: dev
      services:
        perPod:
          remote:
            enabled: false
    ---
    spec:
      storage:
        persistentVolume:
          annotations:
            env: staging
            owner: pool-vc-0
    """
    And I expect 3 statefulsets in 3 kubernetes cluster to be created and eventually ready
    And I expect all 3 RedpandaBrokerPools in "multicluster" to be eventually bound and deployed
    When I execute "rpk redpanda admin brokers list" command in the statefulset container in each cluster
    And I expect them to return the same Redpanda broker list
    # rpk topic list exercises the Kafka listener which uses the "user-provided"
    # cert (SecretRef), validating that the pre-signed TLS secret works.
    And I execute "rpk topic list" command in the statefulset container in each cluster
    # Per-region assertions on the data-dir PVC annotations also
    # implicitly verify the renderer pulled each region's StatefulSet
    # from its own local pool (otherwise vc-1/vc-2 would leak vc-0's
    # override or vice versa).
    Then in "multicluster" the StatefulSet for region "vc-0" of StretchCluster "cluster" should have data-dir PVC annotations:
    """
    team: platform
    env: staging
    owner: pool-vc-0
    """
    And in "multicluster" the StatefulSet for region "vc-1" of StretchCluster "cluster" should have data-dir PVC annotations:
    """
    team: platform
    env: prod
    """
    And in "multicluster" the StatefulSet for region "vc-2" of StretchCluster "cluster" should have data-dir PVC annotations:
    """
    team: platform
    env: prod
    """
