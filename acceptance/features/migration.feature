Feature: Helm chart to Redpanda Operator migration

  @skip:gke @skip:aks @skip:eks
  Scenario: Migrate from a Helm chart release to a Redpanda custom resource
        Given a Helm release named "redpanda-migration-example" of the "redpanda/redpanda" helm chart with the values:
        """
    # tag::helm-values[]
        fullnameOverride: name-override
    # end::helm-values[]
        # Without the below values, the operator would have to modify the cluster after the migration.
        # As this is test specific because we use a local version of the operator, this block is excluded from the helm-values tag above.
        statefulset:
          sideCars:
            image:
              repository: localhost/redpanda-operator
              tag: dev
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

        Then the Redpanda custom resource "redpanda-migration-example" becomes Ready.
        And the StatefulSet "name-override" has an OwnerReference pointing to the Redpanda custom resource "redpanda-migration-example".
        And the "redpanda-migration-example" Helm release can be deleted by removing secret
        And the "redpanda-migration-example" cluster is healthy
        And recorded "Statefulset-Generation" has the same value as "{.metadata.generation}" of "StatefulSet.v1.apps" with "name-override" name
