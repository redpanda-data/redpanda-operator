Feature: operator can migrate/adopt Redpanda helm base deployment

  @skip:gke @skip:aks @skip:eks
  Scenario: Adopt or migrate from stand alone helm chart release to Redpanda custom resource
        Given there is "test-migrate-from-helm" Redpanda helm release with values:
        """
        fullnameOverride: name-override
        """

        When I apply Kubernetes manifest:
        """
    # tag::redpanda-custom-resource-manifest[]
        # This manifest is a copy of Redpanda release helm values
        ---
        apiVersion: cluster.redpanda.com/v1alpha2
        kind: Redpanda
        metadata:
          name: test-migrate-from-helm
        spec:
          clusterSpec:
            fullnameOverride: name-override
    # end::redpanda-custom-resource-manifest[]
        """

        Then "test-migrate-from-helm" Redpanda custom resource is in ready state
