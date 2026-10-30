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
        image:
          repository: ${DEFAULT_REDPANDA_REPO}
          tag: ${DEFAULT_REDPANDA_TAG}
        statefulset:
          replicas: 1
        # The chart pins static nodePorts and this feature needs the external
        # Service, so it claims ports no other feature uses, inside the static
        # band (30000-30085), which auto-allocation only uses once the rest of
        # the range is exhausted.
        listeners:
          admin:
            external:
              default:
                port: 9645
                advertisedPorts: [30010]
          kafka:
            external:
              default:
                advertisedPorts: [30011]
          http:
            external:
              default:
                advertisedPorts: [30012]
          schemaRegistry:
            external:
              default:
                advertisedPorts: [30013]
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
        image:
          repository: ${DEFAULT_REDPANDA_REPO}
          tag: ${DEFAULT_REDPANDA_TAG}
        statefulset:
          replicas: 1
        # The chart pins static nodePorts and this feature needs the external
        # Service, so it claims ports no other feature uses, inside the static
        # band (30000-30085), which auto-allocation only uses once the rest of
        # the range is exhausted.
        listeners:
          admin:
            external:
              default:
                port: 9640
                advertisedPorts: [30010]
          kafka:
            external:
              default:
                advertisedPorts: [30011]
          http:
            external:
              default:
                advertisedPorts: [30012]
          schemaRegistry:
            external:
              default:
                advertisedPorts: [30013]
    """
    Then cluster "upgrade" is stable with 1 nodes
    And service "upgrade-external" should have named port "admin-default" with value 9640
    And rpk is configured correctly in "upgrade" cluster
