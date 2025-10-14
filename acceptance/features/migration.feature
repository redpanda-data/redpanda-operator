Feature: Helm chart to Redpanda Operator migration

  @skip:gke @skip:aks @skip:eks
  Scenario: Migrate from a Helm chart release to a Redpanda custom resource
        Given I helm install "redpanda-migration-example" "../charts/redpanda" with values:
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
        When I apply Kubernetes manifest:
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
        Then cluster "redpanda-migration-example" is available
        And the Kubernetes object of type "StatefulSet.v1.apps" with name "name-override" has an OwnerReference pointing to the cluster "redpanda-migration-example"
        And the helm release for "redpanda-migration-example" can be deleted by removing its stored secret
        And the cluster "redpanda-migration-example" is healthy
