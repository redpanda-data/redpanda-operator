Feature: operator can migrate/adopt Redpanda helm base deployment

  @skip:gke @skip:aks @skip:eks
  Scenario: Adopt or migrate from stand alone helm chart release to Redpanda custom resource
        Given I install a local Redpanda Helm Chart release named "redpanda-migration-example" with values:
        """
    # tag::helm-values[]
        fullnameOverride: name-override
    # end::helm-values[]
        """
        And I store "{.metadata.generation}" of Kubernetes object with type "StatefulSet.v1.apps" and name "name-override" as "generation"
        When I apply Kubernetes manifest:
        """
    # tag::redpanda-custom-resource-manifest[]
        ---
        apiVersion: cluster.redpanda.com/v1alpha2
        kind: Redpanda
        metadata:
          name: redpanda-migration-example
        spec:
          # This manifest is a copy of Redpanda release helm values
          clusterSpec:
            fullnameOverride: name-override
    # end::redpanda-custom-resource-manifest[]
        """
        Then cluster "redpanda-migration-example" is available
        And the Kubernetes object of type "StatefulSet.v1.apps" with name "name-override" has an OwnerReference pointing to the cluster "redpanda-migration-example"
        And the helm release for "redpanda-migration-example" can be deleted by removing its stored secret
        And the cluster "redpanda-migration-example" is healthy
        And the recorded value "generation" has the same value as "{.metadata.generation}" of the Kubernetes object with type "StatefulSet.v1.apps" and name "name-override"