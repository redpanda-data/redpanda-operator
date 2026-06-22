Feature: Helm chart to Redpanda Operator migration

  @skip:gke @skip:aks @skip:eks
  Scenario: Migrate from a Helm chart release to a Redpanda custom resource
        # K8S-658: a cluster configuration property sourced from a Kubernetes Secret via
        # config.extraClusterConfiguration must survive the migration. The Secret is created
        # up front and is never modified while the Helm release is migrated to a Redpanda CR.
        Given I apply Kubernetes manifest:
        """
        ---
        apiVersion: v1
        kind: Secret
        metadata:
          name: iceberg-config
        stringData:
          iceberg_rest_catalog_client_secret: PLACEHOLDER
        """
        And I helm install "redpanda-migration-example" "../charts/redpanda/chart" with values:
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
        # Also test-specific (K8S-658): reference the Secret created above for a cluster config property.
        config:
          extraClusterConfiguration:
            iceberg_rest_catalog_client_secret:
              secretKeyRef:
                name: iceberg-config
                key: iceberg_rest_catalog_client_secret
        """
        And I store "{.metadata.generation}" of Kubernetes object with type "StatefulSet.v1.apps" and name "name-override" as "generation"
        # Before migrating, assert the Secret-referenced property is applied to the running,
        # Helm-managed cluster. iceberg_rest_catalog_client_secret is a Redpanda secret, so the
        # Admin API redacts its value once set; rpk YAML-encodes the redacted "[secret]" marker
        # and single-quotes it because it begins with "[". The rendered bootstrap file instead
        # holds the cleartext resolved from the Secret.
        And I exec "rpk cluster config get iceberg_rest_catalog_client_secret" in a Pod matching "app.kubernetes.io/instance=redpanda-migration-example,app.kubernetes.io/component=redpanda-statefulset", it will output:
        """
        '[secret]'
        """
        And I exec "grep iceberg_rest_catalog_client_secret /etc/redpanda/.bootstrap.yaml" in a Pod matching "app.kubernetes.io/instance=redpanda-migration-example,app.kubernetes.io/component=redpanda-statefulset", it will output:
        """
        iceberg_rest_catalog_client_secret: "PLACEHOLDER"
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
            # The Secret reference is copied verbatim from the Helm values; the Secret is unchanged.
            config:
              extraClusterConfiguration:
                iceberg_rest_catalog_client_secret:
                  secretKeyRef:
                    name: iceberg-config
                    key: iceberg_rest_catalog_client_secret
        """
        Then cluster "redpanda-migration-example" is available
        And the Kubernetes object of type "StatefulSet.v1.apps" with name "name-override" has an OwnerReference pointing to the cluster "redpanda-migration-example"
        And the helm release for "redpanda-migration-example" can be deleted by removing its stored secret
        And the cluster "redpanda-migration-example" is healthy
        # this winds up being incremented due to us forcibly swapping the cluster's StatefulSets to leverage OnDelete semantics
        And the recorded value "generation" is one less than "{.metadata.generation}" of the Kubernetes object with type "StatefulSet.v1.apps" and name "name-override"
        # After the operator has taken over reconciliation, the Secret-referenced property must
        # still be set on the running cluster and still resolved in the bootstrap file.
        And I exec "rpk cluster config get iceberg_rest_catalog_client_secret" in a Pod matching "app.kubernetes.io/instance=redpanda-migration-example,app.kubernetes.io/component=redpanda-statefulset", it will output:
        """
        '[secret]'
        """
        And I exec "grep iceberg_rest_catalog_client_secret /etc/redpanda/.bootstrap.yaml" in a Pod matching "app.kubernetes.io/instance=redpanda-migration-example,app.kubernetes.io/component=redpanda-statefulset", it will output:
        """
        iceberg_rest_catalog_client_secret: "PLACEHOLDER"
        """
