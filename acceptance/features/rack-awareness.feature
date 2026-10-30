Feature: Rack Awareness
  @skip:gke @skip:aks @skip:eks
  Scenario: Rack Awareness
    Given I apply Kubernetes manifest:
    # NB: You wouldn't actually use kubernetes.io/os for the value of rack,
    # it's just a value that we know is both present and deterministic for the
    # purpose of testing.
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Redpanda
    metadata:
      name: rack-awareness
    spec:
      clusterSpec:
        # The chart pins static nodePorts (e.g. 31644); a second cluster with
        # external enabled on the shared k3d cluster fails to create its Service.
        external:
          enabled: false
        image:
          repository: ${DEFAULT_REDPANDA_REPO}
          tag: ${DEFAULT_REDPANDA_TAG}
        console:
          enabled: false
        statefulset:
          replicas: 1
        rackAwareness:
          enabled: true
          nodeAnnotation: 'kubernetes.io/os'
    """
    And cluster "rack-awareness" is stable with 1 nodes
    Then running `cat /etc/redpanda/redpanda.yaml | grep -o 'rack: .*$'` will output:
    """
    rack: linux
    """
