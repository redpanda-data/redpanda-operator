Feature: Vectorized Cluster

  # Tests scaling a v1alpha1 Cluster up and down to verify decommissioning works correctly
  @skip:gke @skip:aks @skip:eks
  Scenario: Vectorized Scaling
    Given I apply Kubernetes manifest:
    """
    ---
    apiVersion: redpanda.vectorized.io/v1alpha1
    kind: Cluster
    metadata:
      name: decommission
    spec:
      image: ${DEFAULT_REDPANDA_REPO}
      version: ${DEFAULT_REDPANDA_TAG}
      replicas: 3
      resources:
        requests:
          cpu: "100m"
          memory: 256Mi
        limits:
          cpu: "100m"
          memory: 256Mi
      configuration:
        rpcServer:
          port: 33145
        kafkaApi:
          - port: 9092
        adminApi:
          - port: 9644
        pandaproxyApi:
          - port: 8082
        developerMode: true
    """
    And vectorized cluster "decommission" is available
    And Pod "decommission-0" is eventually be Running
    Then '{.status.replicas}' of Cluster.v1alpha1.redpanda.vectorized.io "decommission" will be '3'
    And '{.status.currentReplicas}' of Cluster.v1alpha1.redpanda.vectorized.io "decommission" will be '3'
    And '{.status.readyReplicas}' of Cluster.v1alpha1.redpanda.vectorized.io "decommission" will be '3'
    And '{.spec.persistentVolumeClaimRetentionPolicy}' of StatefulSet.v1.apps "decommission" will be '{"whenDeleted": "Delete", "whenScaled": "Delete"}'
    # TODO(chrisseto): kubectl apply --server-side doesn't merge partials of
    # vectorized clusters, it just nulls out portions of it. We re-specify the
    # entire spec to work around the limitation for now.
    # First scale down from 3 to 2
    When I apply Kubernetes manifest:
    """
    ---
    apiVersion: redpanda.vectorized.io/v1alpha1
    kind: Cluster
    metadata:
      name: decommission
    spec:
      image: ${DEFAULT_REDPANDA_REPO}
      version: ${DEFAULT_REDPANDA_TAG}
      replicas: 2 # <-- Change to replicas
      resources:
        requests:
          cpu: "100m"
          memory: 256Mi
        limits:
          cpu: "100m"
          memory: 256Mi
      configuration:
        rpcServer:
          port: 33145
        kafkaApi:
          - port: 9092
        adminApi:
          - port: 9644
        pandaproxyApi:
          - port: 8082
        developerMode: true
    """
    Then vectorized cluster "decommission" is available
    Then '{.status.replicas}' of Cluster.v1alpha1.redpanda.vectorized.io "decommission" will be '2'
    And '{.status.currentReplicas}' of Cluster.v1alpha1.redpanda.vectorized.io "decommission" will be '2'
    And '{.status.readyReplicas}' of Cluster.v1alpha1.redpanda.vectorized.io "decommission" will be '2'
    And kubectl exec -it "decommission-0" "rpk redpanda admin brokers list | sed -E 's/\s+/ /gm' | cut -d ' ' -f 1,6" will eventually output:
    """
    ID MEMBERSHIP
    0 active
    1 active
    """
    # Scale back up to 3
    When I apply Kubernetes manifest:
    """
    ---
    apiVersion: redpanda.vectorized.io/v1alpha1
    kind: Cluster
    metadata:
      name: decommission
    spec:
      image: ${DEFAULT_REDPANDA_REPO}
      version: ${DEFAULT_REDPANDA_TAG}
      replicas: 3 # <-- Change to replicas
      resources:
        requests:
          cpu: "100m"
          memory: 256Mi
        limits:
          cpu: "100m"
          memory: 256Mi
      configuration:
        rpcServer:
          port: 33145
        kafkaApi:
          - port: 9092
        adminApi:
          - port: 9644
        pandaproxyApi:
          - port: 8082
        developerMode: true
    """
    And vectorized cluster "decommission" is available
    Then '{.status.readyReplicas}' of Cluster.v1alpha1.redpanda.vectorized.io "decommission" will be '3'
    And kubectl exec -it "decommission-0" "rpk redpanda admin brokers list | sed -E 's/\s+/ /gm' | cut -d ' ' -f 1,6" will eventually output:
    """
    ID MEMBERSHIP
    0 active
    1 active
    3 active
    """
    # Scale down again to verify decommissioning works repeatedly
    When I apply Kubernetes manifest:
    """
    ---
    apiVersion: redpanda.vectorized.io/v1alpha1
    kind: Cluster
    metadata:
      name: decommission
    spec:
      image: ${DEFAULT_REDPANDA_REPO}
      version: ${DEFAULT_REDPANDA_TAG}
      replicas: 2 # <-- Change to replicas
      resources:
        requests:
          cpu: "100m"
          memory: 256Mi
        limits:
          cpu: "100m"
          memory: 256Mi
      configuration:
        rpcServer:
          port: 33145
        kafkaApi:
          - port: 9092
        adminApi:
          - port: 9644
        pandaproxyApi:
          - port: 8082
        developerMode: true
    """
    And vectorized cluster "decommission" is available
    Then '{.status.readyReplicas}' of Cluster.v1alpha1.redpanda.vectorized.io "decommission" will be '2'
    And kubectl exec -it "decommission-0" "rpk redpanda admin brokers list | sed -E 's/\s+/ /gm' | cut -d ' ' -f 1,6" will eventually output:
    """
    ID MEMBERSHIP
    0 active
    1 active
    """

  # Tests deleting nodepools from a v1alpha1 Cluster without scaling to zero first
  # Verifies that the operator properly decommissions nodes incrementally (3->2->1->0->gone)
  @skip:gke @skip:aks @skip:eks
  Scenario: Vectorized NodePool Deletion
    Given I apply Kubernetes manifest:
    """
    ---
    apiVersion: redpanda.vectorized.io/v1alpha1
    kind: Cluster
    metadata:
      name: nodepools-delete
    spec:
      image: ${DEFAULT_REDPANDA_REPO}
      version: ${DEFAULT_REDPANDA_TAG}
      nodePools:
        - name: first
          replicas: 2
          storage: {}
          cloudCacheStorage: {}
          resources:
            requests:
              cpu: "100m"
              memory: 256Mi
            limits:
              cpu: "100m"
              memory: 256Mi
      configuration:
        rpcServer:
          port: 33145
        kafkaApi:
          - port: 9092
        adminApi:
          - port: 9644
        developerMode: true
      resources: {}
    """
    And vectorized cluster "nodepools-delete" is available
    Then '{.status.replicas}' of Cluster.v1alpha1.redpanda.vectorized.io "nodepools-delete" will be '2'
    And '{.status.currentReplicas}' of Cluster.v1alpha1.redpanda.vectorized.io "nodepools-delete" will be '2'
    And '{.status.readyReplicas}' of Cluster.v1alpha1.redpanda.vectorized.io "nodepools-delete" will be '2'
    And '{.status.nodePools.first}' of Cluster.v1alpha1.redpanda.vectorized.io "nodepools-delete" will be:
    """
    {"currentReplicas":2,"replicas":2,"readyReplicas":2,"restarting":false}
    """

    # Add a second nodepool with slightly different resources
    When I apply Kubernetes manifest:
    """
    ---
    apiVersion: redpanda.vectorized.io/v1alpha1
    kind: Cluster
    metadata:
      name: nodepools-delete
    spec:
      image: ${DEFAULT_REDPANDA_REPO}
      version: ${DEFAULT_REDPANDA_TAG}
      nodePools:
        - name: first
          replicas: 2
          storage: {}
          cloudCacheStorage: {}
          resources:
            requests:
              cpu: "100m"
              memory: 256Mi
            limits:
              cpu: "100m"
              memory: 256Mi
        - name: second
          replicas: 2
          storage: {}
          cloudCacheStorage: {}
          resources:
            requests:
              cpu: "101m"
              memory: 257Mi
            limits:
              cpu: "101m"
              memory: 257Mi
      configuration:
        rpcServer:
          port: 33145
        kafkaApi:
          - port: 9092
        adminApi:
          - port: 9644
        developerMode: true
    """
    Then vectorized cluster "nodepools-delete" is available
    And '{.status.replicas}' of Cluster.v1alpha1.redpanda.vectorized.io "nodepools-delete" will be '4'
    And '{.status.nodePools.first}' of Cluster.v1alpha1.redpanda.vectorized.io "nodepools-delete" will be:
    """
    {"currentReplicas":2,"replicas":2,"readyReplicas":2,"restarting":false}
    """
    And '{.status.nodePools.second}' of Cluster.v1alpha1.redpanda.vectorized.io "nodepools-delete" will be:
    """
    {"currentReplicas":2,"replicas":2,"readyReplicas":2,"restarting":false}
    """

    # Delete the first nodepool by removing it from the spec entirely
    # WITHOUT scaling to zero first - should go 3->2->1->0->gone
    When I apply Kubernetes manifest:
    """
    ---
    apiVersion: redpanda.vectorized.io/v1alpha1
    kind: Cluster
    metadata:
      name: nodepools-delete
    spec:
      image: ${DEFAULT_REDPANDA_REPO}
      version: ${DEFAULT_REDPANDA_TAG}
      nodePools:
        - name: second
          replicas: 2
          storage: {}
          cloudCacheStorage: {}
          resources:
            requests:
              cpu: "101m"
              memory: 257Mi
            limits:
              cpu: "101m"
              memory: 257Mi
      configuration:
        rpcServer:
          port: 33145
        kafkaApi:
          - port: 9092
        adminApi:
          - port: 9644
        developerMode: true
        additionalCommandlineArguments:
          dump-memory-diagnostics-on-alloc-failure-kind: all
          abort-on-seastar-bad-alloc: ''
      resources: {}
    """
    Then vectorized cluster "nodepools-delete" is available
    And '{.status.replicas}' of Cluster.v1alpha1.redpanda.vectorized.io "nodepools-delete" will be '2'
    And '{.status.nodePools}' of Cluster.v1alpha1.redpanda.vectorized.io "nodepools-delete" will be:
    """
    {"second":{"currentReplicas":2,"replicas":2,"readyReplicas":2,"restarting":false}}
    """
    And StatefulSet.v1.apps "nodepools-delete-first" is eventually deleted

  # Tests scaling nodepools in a v1alpha1 Cluster
  @skip:gke @skip:aks @skip:eks
  Scenario: Vectorized NodePool Scaling
    Given I apply Kubernetes manifest:
    """
    ---
    apiVersion: redpanda.vectorized.io/v1alpha1
    kind: Cluster
    metadata:
      name: nodepool-cluster
    spec:
      image: ${DEFAULT_REDPANDA_REPO}
      version: ${DEFAULT_REDPANDA_TAG}
      nodePools:
        - name: nodepool1
          replicas: 2
          storage: {}
          cloudCacheStorage: {}
          resources:
            requests:
              cpu: "100m"
              memory: 256Mi
            limits:
              cpu: "100m"
              memory: 256Mi
      configuration:
        rpcServer:
          port: 33145
        kafkaApi:
          - port: 9092
        adminApi:
          - port: 9644
        developerMode: true
        additionalCommandlineArguments:
          dump-memory-diagnostics-on-alloc-failure-kind: all
          abort-on-seastar-bad-alloc: ''
      resources: {}
    """
    And vectorized cluster "nodepool-cluster" is available
    Then '{.status.replicas}' of Cluster.v1alpha1.redpanda.vectorized.io "nodepool-cluster" will be '2'
    And '{.status.currentReplicas}' of Cluster.v1alpha1.redpanda.vectorized.io "nodepool-cluster" will be '2'
    And '{.status.readyReplicas}' of Cluster.v1alpha1.redpanda.vectorized.io "nodepool-cluster" will be '2'
    And '{.status.restarting}' of Cluster.v1alpha1.redpanda.vectorized.io "nodepool-cluster" will be 'false'
    And '{.status.nodePools.nodepool1}' of Cluster.v1alpha1.redpanda.vectorized.io "nodepool-cluster" will be:
    """
    {"currentReplicas":2,"replicas":2,"readyReplicas":2,"restarting":false}
    """

    # Add a second nodepool
    When I apply Kubernetes manifest:
    """
    ---
    apiVersion: redpanda.vectorized.io/v1alpha1
    kind: Cluster
    metadata:
      name: nodepool-cluster
    spec:
      image: ${DEFAULT_REDPANDA_REPO}
      version: ${DEFAULT_REDPANDA_TAG}
      nodePools:
        - name: nodepool1
          replicas: 2
          storage: {}
          cloudCacheStorage: {}
          resources:
            requests:
              cpu: "100m"
              memory: 256Mi
            limits:
              cpu: "100m"
              memory: 256Mi
        - name: nodepool2
          replicas: 2
          storage: {}
          cloudCacheStorage: {}
          resources:
            requests:
              cpu: "100m"
              memory: 256Mi
            limits:
              cpu: "100m"
              memory: 256Mi
      configuration:
        rpcServer:
          port: 33145
        kafkaApi:
          - port: 9092
        adminApi:
          - port: 9644
        developerMode: true
        additionalCommandlineArguments:
          dump-memory-diagnostics-on-alloc-failure-kind: all
          abort-on-seastar-bad-alloc: ''
      resources: {}
    """
    Then vectorized cluster "nodepool-cluster" is available
    And '{.status.replicas}' of Cluster.v1alpha1.redpanda.vectorized.io "nodepool-cluster" will be '4'
    And '{.status.nodePools.nodepool1}' of Cluster.v1alpha1.redpanda.vectorized.io "nodepool-cluster" will be:
    """
    {"currentReplicas":2,"replicas":2,"readyReplicas":2,"restarting":false}
    """
    And '{.status.nodePools.nodepool2}' of Cluster.v1alpha1.redpanda.vectorized.io "nodepool-cluster" will be:
    """
    {"currentReplicas":2,"replicas":2,"readyReplicas":2,"restarting":false}
    """

    # Scale down the first nodepool to 0
    When I apply Kubernetes manifest:
    """
    ---
    apiVersion: redpanda.vectorized.io/v1alpha1
    kind: Cluster
    metadata:
      name: nodepool-cluster
    spec:
      image: ${DEFAULT_REDPANDA_REPO}
      version: ${DEFAULT_REDPANDA_TAG}
      nodePools:
        - name: nodepool1
          replicas: 0
          storage: {}
          cloudCacheStorage: {}
          resources:
            requests:
              cpu: "100m"
              memory: 256Mi
            limits:
              cpu: "100m"
              memory: 256Mi
        - name: nodepool2
          replicas: 3
          storage: {}
          cloudCacheStorage: {}
          resources:
            requests:
              cpu: "100m"
              memory: 256Mi
            limits:
              cpu: "100m"
              memory: 256Mi
      configuration:
        rpcServer:
          port: 33145
        kafkaApi:
          - port: 9092
        adminApi:
          - port: 9644
        developerMode: true
        additionalCommandlineArguments:
          dump-memory-diagnostics-on-alloc-failure-kind: all
          abort-on-seastar-bad-alloc: ''
      resources: {}
    """
    Then vectorized cluster "nodepool-cluster" is available
    And '{.status.replicas}' of Cluster.v1alpha1.redpanda.vectorized.io "nodepool-cluster" will be '2'
    And '{.status.nodePools.nodepool1}' of Cluster.v1alpha1.redpanda.vectorized.io "nodepool-cluster" will be:
    """
    {"currentReplicas":0,"replicas":0,"readyReplicas":0,"restarting":false}
    """
    And '{.status.nodePools.nodepool2}' of Cluster.v1alpha1.redpanda.vectorized.io "nodepool-cluster" will be:
    """
    {"currentReplicas":2,"replicas":2,"readyReplicas":2,"restarting":false}
    """

  # Tests that v1alpha1 Clusters can tolerate node failures and properly decommission ghost brokers
  @skip:gke @skip:aks @skip:eks
  Scenario: Vectorized Tolerating Node Failures
    Given I apply Kubernetes manifest:
    """
    ---
    apiVersion: redpanda.vectorized.io/v1alpha1
    kind: Cluster
    metadata:
      name: node-failure
    spec:
      image: ${DEFAULT_REDPANDA_REPO}
      version: ${DEFAULT_REDPANDA_TAG}
      replicas: 3
      resources:
        requests:
          cpu: "100m"
          memory: 256Mi
        limits:
          cpu: "100m"
          memory: 256Mi
      configuration:
        rpcServer:
          port: 33145
        kafkaApi:
          - port: 9092
        adminApi:
          - port: 9644
        pandaproxyApi:
          - port: 8082
        developerMode: true
    """
    And vectorized cluster "node-failure" is available
    When I stop the Node running Pod "node-failure-2"
    And Pod "node-failure-2" is eventually Pending
    Then Pod "node-failure-2" will eventually be Running
    And kubectl exec -it "node-failure-0" "rpk redpanda admin brokers list | sed -E 's/\s+/ /gm' | cut -d ' ' -f 1,6" will eventually output:
    """
    ID MEMBERSHIP
    0 active
    1 active
    3 active
    """
    And kubectl exec -it "node-failure-0" "rpk redpanda admin brokers list --include-decommissioned | sed -E 's/\s+/ /gm' | cut -d ' ' -f 1,6" will eventually output:
    """
    ID MEMBERSHIP
    0 active
    1 active
    3 active
    2 -
    """
