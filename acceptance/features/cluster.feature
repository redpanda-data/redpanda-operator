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
        # Per-feature nodePorts; see nodePortOffsets in acceptance/steps.
        listeners:
          admin:
            external:
              default:
                port: 9645
                advertisedPorts:
                - ${NODEPORT_ADMIN}
          kafka:
            external:
              default:
                advertisedPorts:
                - ${NODEPORT_KAFKA}
          http:
            external:
              default:
                advertisedPorts:
                - ${NODEPORT_HTTP}
          schemaRegistry:
            external:
              default:
                advertisedPorts:
                - ${NODEPORT_SR}
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
        # Per-feature nodePorts; see nodePortOffsets in acceptance/steps.
        listeners:
          admin:
            external:
              default:
                port: 9640
                advertisedPorts:
                - ${NODEPORT_ADMIN}
          kafka:
            external:
              default:
                advertisedPorts:
                - ${NODEPORT_KAFKA}
          http:
            external:
              default:
                advertisedPorts:
                - ${NODEPORT_HTTP}
          schemaRegistry:
            external:
              default:
                advertisedPorts:
                - ${NODEPORT_SR}
    """
    Then cluster "upgrade" is stable with 1 nodes
    And service "upgrade-external" should have named port "admin-default" with value 9640
    And rpk is configured correctly in "upgrade" cluster
