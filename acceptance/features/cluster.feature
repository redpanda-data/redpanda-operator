# This file contains some tests originally ported from our e2e-v2 tests.
# We should really evaluate whether or not to just delete these.
Feature: Basic cluster tests
  @skip:gke @skip:aks @skip:eks
  Scenario: Updating admin ports
    # replaces e2e-v2 "upgrade-values-check"   
    Given I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Redpanda
    metadata:
      name: upgrade
    spec:
      clusterSpec:
        statefulset:
          replicas: 1
        listeners:
          admin:
            external:
              default:
                port: 9645
    """
    And cluster "upgrade" is stable with 1 nodes
    And service "upgrade-external" has named port "admin-default" with value 9645
    And rpk is configured correctly in "upgrade" cluster
    When I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Redpanda
    metadata:
      name: upgrade
    spec:
      clusterSpec:
        statefulset:
          replicas: 1
        listeners:
          admin:
            external:
              default:
                port: 9640
    """
    Then cluster "upgrade" is stable with 1 nodes
    And service "upgrade-external" should have named port "admin-default" with value 9640
    And rpk is configured correctly in "upgrade" cluster


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
