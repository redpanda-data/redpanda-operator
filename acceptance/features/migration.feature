Feature: operator can migrate/adopt Redpanda helm base deployment

  @skip:gke @skip:aks @skip:eks
  Scenario: Adopt or migrate from stand alone helm chart release to Redpanda custom resource
        Given a Helm release named "redpanda-migration-example" of local Redpanda Helm Chart with values:
        """
    # tag::helm-values[]
        fullnameOverride: name-override
    # end::helm-values[]
        """

        When I record "{.metadata.generation}" of "StatefulSet.v1.apps" with "name-override" name as "Statefulset-Generation"
        When I apply the following Redpanda custom resource manifest for migration:
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

        Then the Redpanda custom resource "redpanda-migration-example" becomes Ready.
        Then the StatefulSet "name-override" has an OwnerReference pointing to the Redpanda custom resource "redpanda-migration-example".
        Then "redpanda-migration-example" helm release can be deleted by removing secret
        Then "redpanda-migration-example" Redpanda cluster is healthy
        Then recorded "Statefulset-Generation" has the same value as "{.metadata.generation}" of "StatefulSet.v1.apps" with "name-override" name