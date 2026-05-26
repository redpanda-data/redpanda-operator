@multicluster
Feature: Stretch Cluster Layered CRDs
  # Happy-path coverage that Topic / RedpandaRole / Schema CRs whose
  # spec.cluster.clusterRef points at a StretchCluster reach a Synced
  # status when the multicluster operator is running, and that the
  # resulting object is observable from the brokers via rpk. This is the
  # GA surface for layered CR support — see the Setup*ControllerForMulticluster
  # wiring in operator/cmd/multicluster.
  #
  # We deliberately apply the CRs to every vcluster (mirroring how the
  # StretchCluster itself is replicated) so the same scenario also
  # exercises the per-peer `req.ClusterName` plumbing on the factory —
  # nothing in production requires the CR to exist on every cluster, but
  # checking against all three is the same wall-clock cost.
  #
  # Topic / RedpandaRole / Schema cover the three distinct client paths
  # the factory's StretchCluster handling added (Kafka, Admin API, Schema
  # Registry). User / Group / ShadowLink go through the same factory paths
  # and would require either SASL bootstrapping or a remote source cluster
  # to drive end-to-end, so they're intentionally left out of this smoke
  # test — the unit-level CR wiring is covered by Setup*ControllerForMulticluster
  # for those types alongside the three exercised here.

  @skip:gke @skip:aks @skip:eks
  Scenario: Layered CRs sync against a StretchCluster
    Given I create a multicluster operator named "layered" with 3 nodes
    And I apply a multicluster Kubernetes manifest to "layered":
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: StretchCluster
    metadata:
      name: cluster
      namespace: default
    spec: {}
    """
    And I apply a RedpandaBrokerPool Kubernetes manifest to "layered":
    """
    spec:
      external:
        enabled: false
      rbac:
        enabled: true
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
    """
    And I expect 3 statefulsets in 3 kubernetes cluster to be created and eventually ready
    And I expect all 3 RedpandaBrokerPools in "layered" to be eventually bound and deployed

    # Topic — Topic uses its own ReadyCondition (status True / reason Succeeded).
    When I apply a multicluster Kubernetes manifest to "layered":
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Topic
    metadata:
      name: stretch-topic
      namespace: default
    spec:
      cluster:
        clusterRef:
          group: cluster.redpanda.com
          kind: StretchCluster
          name: cluster
      partitions: 1
      replicationFactor: 1
    """
    Then in "layered" the Kubernetes object "stretch-topic" in namespace "default" of type "Topic.v1alpha2.cluster.redpanda.com" should have condition "Ready" with status "True"
    And I execute "rpk topic describe stretch-topic" command in the statefulset container in each cluster

    # RedpandaRole — generic resource controller, reports a Synced condition.
    When I apply a multicluster Kubernetes manifest to "layered":
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: RedpandaRole
    metadata:
      name: stretch-role
      namespace: default
    spec:
      cluster:
        clusterRef:
          group: cluster.redpanda.com
          kind: StretchCluster
          name: cluster
    """
    Then in "layered" the Kubernetes object "stretch-role" in namespace "default" of type "RedpandaRole.v1alpha2.cluster.redpanda.com" should have condition "Synced" with status "True"
    And I execute "rpk security role describe stretch-role" command in the statefulset container in each cluster

    # Schema — exercises the Schema Registry endpoint discovery added to the
    # factory's StretchCluster path.
    When I apply a multicluster Kubernetes manifest to "layered":
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Schema
    metadata:
      name: stretch-schema
      namespace: default
    spec:
      cluster:
        clusterRef:
          group: cluster.redpanda.com
          kind: StretchCluster
          name: cluster
      schemaType: avro
      text: |
        {
          "type": "record",
          "name": "Event",
          "fields": [
            { "type": "string", "name": "id" }
          ]
        }
    """
    Then in "layered" the Kubernetes object "stretch-schema" in namespace "default" of type "Schema.v1alpha2.cluster.redpanda.com" should have condition "Synced" with status "True"
    And I execute "rpk registry schema get stretch-schema --schema-version latest" command in the statefulset container in each cluster
