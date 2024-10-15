@cluster:basic
Feature: Schema CRDs
  Background: Cluster available
    Given cluster "basic" is available

  @skip:gke @skip:aks @skip:eks
  Scenario: Managing Schemas
    Given there is no schema "schema1" in cluster "basic"
    When I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Schema
    metadata:
        name: schema1
    spec:
        cluster:
            clusterRef:
                name: basic
        text: |
            {
                "type": "record",
                "name": "test",
                "fields":
                [
                    {
                        "type": "string",
                        "name": "field1"
                    },
                    {
                        "type": "int",
                        "name": "field2"
                    }
                ]
            }
    """
    And schema "schema1" is successfully synced
    Then I should be able to check compatibility against "schema1" in cluster "basic"