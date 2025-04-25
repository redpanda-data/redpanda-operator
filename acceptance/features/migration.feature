Feature: Helm chart to Redpanda Operator migration

  @skip:gke @skip:aks @skip:eks
  Scenario: Migrate from a Helm chart release to a Redpanda custom resource
        Given a Helm release named "redpanda-migration-example" of the "redpanda/redpanda" helm chart with the values:
        """
    # tag::helm-values[]
        fullnameOverride: name-override
    # end::helm-values[]
        """

        When I record "{.metadata.generation}" of "StatefulSet.v1.apps" with "name-override" name as "Statefulset-Generation"
        And I apply the following Redpanda custom resource manifest for migration:
        """
    # tag::redpanda-custom-resource-manifest[]
        ---
        apiVersion: cluster.redpanda.com/v1alpha2
        kind: Redpanda
        metadata:
          name: redpanda-migration-example
        spec:
          # This manifest is a copy of Redpanda release Helm values
          clusterSpec:
            fullnameOverride: name-override
    # end::redpanda-custom-resource-manifest[]
        """
        And the Redpanda custom resource "redpanda-migration-example" becomes Ready.
        And "redpanda-migration-example" Helm release is deleted by removing secret

        Then the StatefulSet "name-override" has an OwnerReference pointing to the Redpanda custom resource "redpanda-migration-example".
        And the "redpanda-migration-example" cluster is healthy
        And the recorded "Statefulset-Generation" matches the current "{.metadata.generation}" field of the "StatefulSet.v1.apps" resource named "name-override"