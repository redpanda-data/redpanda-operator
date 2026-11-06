Feature: Migrating NodePools to RedpandaBrokerPools
  # A RedpandaBrokerPool with a NodePool's name and spec takes the NodePool's
  # pool, StatefulSet and brokers included, once the NodePool is annotated with
  # operator.redpanda.com/migrate-to-broker-pool or deleted. The StatefulSet
  # renders the same either way, so no broker restarts. Each scenario runs its
  # own operator in a vcluster: NodePools need --enable-v2-nodepools, and the
  # license scenario needs an operator without a license.

  @vcluster @skip:gke @skip:aks @skip:eks
  Scenario: Annotating a NodePool hands its pool to a RedpandaBrokerPool without restarting brokers
    Given the enterprise license is available as Secret "redpanda-license"
    And I helm install "redpanda-operator" "../operator/chart" with values:
    """
    image:
      tag: dev
      repository: localhost/redpanda-operator
    crds:
      enabled: true
      experimental: true
    enterprise:
      licenseSecretRef:
        name: redpanda-license
        key: redpanda.license
    additionalCmdFlags:
    - --enable-v2-nodepools
    """
    And I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Redpanda
    metadata:
      name: np-migration
    spec:
      clusterSpec:
        image:
          repository: ${DEFAULT_REDPANDA_REPO}
          tag: ${DEFAULT_REDPANDA_TAG}
        external:
          enabled: false
        statefulset:
          replicas: 1
          sideCars:
            image:
              tag: dev
              repository: localhost/redpanda-operator
            controllers:
              image:
                tag: dev
                repository: localhost/redpanda-operator
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: NodePool
    metadata:
      name: blue
    spec:
      clusterRef:
        name: np-migration
      replicas: 1
      sidecarImage:
        repository: localhost/redpanda-operator
        tag: dev
    """
    And cluster "np-migration" is stable with 2 nodes
    And NodePool "blue" should eventually have condition "Deployed" with reason "Deployed"
    When I snapshot pod UIDs for cluster "np-migration"
    # The NodePool keeps its pool while a RedpandaBrokerPool waits to take it,
    # even one whose spec would scale the pool.
    And I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: RedpandaBrokerPool
    metadata:
      name: blue
    spec:
      clusterRef:
        name: np-migration
      replicas: 2
      sidecarImage:
        repository: localhost/redpanda-operator
        tag: dev
    """
    Then RedpandaBrokerPool "blue" should eventually have condition "Deployed" with reason "SupersededByNodePool" and a message containing "The specs differ in replicas"
    And the StatefulSet for pool "blue" of cluster "np-migration" should keep 1 replica for 30 seconds
    # Matching the spec bumps the RedpandaBrokerPool to generation 2, which the
    # StatefulSet's generation label then shows once the handoff renders it.
    When I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: RedpandaBrokerPool
    metadata:
      name: blue
    spec:
      clusterRef:
        name: np-migration
      replicas: 1
      sidecarImage:
        repository: localhost/redpanda-operator
        tag: dev
    """
    Then RedpandaBrokerPool "blue" should eventually have condition "Deployed" with reason "SupersededByNodePool" and a message containing "The specs match"
    When I set annotation "operator.redpanda.com/migrate-to-broker-pool" to "true" on NodePool "blue"
    Then the StatefulSet for pool "blue" of cluster "np-migration" should eventually be rendered from generation 2
    And RedpandaBrokerPool "blue" should eventually have condition "Deployed" with reason "Deployed"
    And NodePool "blue" should eventually have condition "Deployed" with reason "Deployed" and a message containing "Pool is owned by RedpandaBrokerPool"
    And cluster "np-migration" should be stable with 2 nodes
    And pods for cluster "np-migration" should have the same UIDs as the snapshot
    # Once the pool is handed over, deleting the NodePool leaves it alone.
    When I delete NodePool "blue"
    Then the StatefulSet for pool "blue" of cluster "np-migration" should keep 1 replica for 30 seconds
    And cluster "np-migration" should be stable with 2 nodes
    And pods for cluster "np-migration" should have the same UIDs as the snapshot
    # The RedpandaBrokerPool now owns the pool's brokers.
    When I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: RedpandaBrokerPool
    metadata:
      name: blue
    spec:
      clusterRef:
        name: np-migration
      replicas: 2
      sidecarImage:
        repository: localhost/redpanda-operator
        tag: dev
    """
    Then cluster "np-migration" should be stable with 3 nodes

  @vcluster @skip:gke @skip:aks @skip:eks
  Scenario: Without a license, deleting a NodePool freezes its pool instead of removing it
    Given the enterprise license is available as Secret "redpanda-license"
    And I helm install "redpanda-operator" "../operator/chart" with values:
    """
    image:
      tag: dev
      repository: localhost/redpanda-operator
    crds:
      enabled: true
      experimental: true
    additionalCmdFlags:
    - --enable-v2-nodepools
    """
    And I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Redpanda
    metadata:
      name: np-migration
    spec:
      clusterSpec:
        image:
          repository: ${DEFAULT_REDPANDA_REPO}
          tag: ${DEFAULT_REDPANDA_TAG}
        external:
          enabled: false
        statefulset:
          replicas: 1
          sideCars:
            image:
              tag: dev
              repository: localhost/redpanda-operator
            controllers:
              image:
                tag: dev
                repository: localhost/redpanda-operator
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: NodePool
    metadata:
      name: blue
    spec:
      clusterRef:
        name: np-migration
      replicas: 1
      sidecarImage:
        repository: localhost/redpanda-operator
        tag: dev
    """
    And cluster "np-migration" is stable with 2 nodes
    When I snapshot pod UIDs for cluster "np-migration"
    And I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: RedpandaBrokerPool
    metadata:
      name: blue
    spec:
      clusterRef:
        name: np-migration
      replicas: 1
      sidecarImage:
        repository: localhost/redpanda-operator
        tag: dev
    """
    Then RedpandaBrokerPool "blue" should eventually have condition "Deployed" with reason "LicenseInvalid" and a message containing "no license configured"
    # Deleting the NodePool hands the pool to the unlicensed RedpandaBrokerPool.
    # The operator freezes the cluster's brokers rather than dropping the pool,
    # which would decommission them.
    When I delete NodePool "blue"
    Then Redpanda "np-migration" should eventually have condition "ResourcesSynced" with reason "TerminalError" and a message containing "RedpandaBrokerPools blue require an enterprise license"
    And the StatefulSet for pool "blue" of cluster "np-migration" should keep 1 replica for 60 seconds
    And pods for cluster "np-migration" should have the same UIDs as the snapshot
    # Licensing the operator resumes management with the pool intact.
    When I helm upgrade "redpanda-operator" "../operator/chart" with values:
    """
    image:
      tag: dev
      repository: localhost/redpanda-operator
    crds:
      enabled: true
      experimental: true
    enterprise:
      licenseSecretRef:
        name: redpanda-license
        key: redpanda.license
    additionalCmdFlags:
    - --enable-v2-nodepools
    """
    Then RedpandaBrokerPool "blue" should eventually have condition "Deployed" with reason "Deployed"
    And cluster "np-migration" should be stable with 2 nodes
    And pods for cluster "np-migration" should have the same UIDs as the snapshot
