Feature: Metrics endpoint has authentication and authorization

  @skip:gke @skip:aks @skip:eks
  Scenario: Reject request without TLS
    Given the operator is running
    Then its metrics endpoint should reject http request with status code "400"

  @skip:gke @skip:aks @skip:eks
  Scenario: Reject unauthenticated token
    Given the operator is running
    Then its metrics endpoint should reject authorization random token request with status code "500"

  @skip:gke @skip:aks @skip:eks
  Scenario: Accept request
    Given the operator is running
    When I apply Kubernetes manifest:
    """
    apiVersion: v1
    kind: ServiceAccount
    metadata:
      name: testing
    """
    And "testing" service account has bounded "redpanda-operator-metrics-reader" cluster role
    Then its metrics endpoint should accept https request with "testing" service account token


