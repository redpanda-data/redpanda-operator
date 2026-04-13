@cluster:basic @variant:vectorized
Feature: Schema CRDs
  Background: Cluster available
    Given cluster "basic" is available

  @skip:gke @skip:aks @skip:eks
  Scenario: Manage customer profile schema (Avro)
    Given there is no schema "customer-profile" in cluster "basic"
    When I apply Kubernetes manifest:
    """
# tag::customer-profile-avro-schema-manifest[]
    # This manifest creates an Avro schema named "customer-profile" in the "basic" cluster.
    # The schema defines a record with fields for customer ID, name, and age.
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Schema
    metadata:
      name: customer-profile
    spec:
      cluster:
        clusterRef:
          name: basic
      schemaType: avro
      compatibilityLevel: Backward
      text: |
        {
          "type": "record",
          "name": "CustomerProfile",
          "fields": [
            { "type": "string", "name": "customer_id" },
            { "type": "string", "name": "name" },
            { "type": "int", "name": "age" }
          ]
        }
# end::customer-profile-avro-schema-manifest[]
    """
    And schema "customer-profile" is successfully synced
    Then I should be able to check compatibility against "customer-profile" in cluster "basic"

  @skip:gke @skip:aks @skip:eks
  Scenario: Manage product catalog schema (Protobuf)
    Given there is no schema "product-catalog" in cluster "basic"
    When I apply Kubernetes manifest:
    """
# tag::product-catalog-protobuf-schema-manifest[]
    # This manifest creates a Protobuf schema named "product-catalog" in the "basic" cluster.
    # The schema defines a message "Product" with fields for product ID, name, price, and category.
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Schema
    metadata:
      name: product-catalog
    spec:
      cluster:
        clusterRef:
          name: basic
      schemaType: protobuf
      compatibilityLevel: Backward
      text: |
        syntax = "proto3";

        message Product {
          int32 product_id = 1;
          string product_name = 2;
          double price = 3;
          string category = 4;
        }
# end::product-catalog-protobuf-schema-manifest[]
    """
    And schema "product-catalog" is successfully synced
    Then I should be able to check compatibility against "product-catalog" in cluster "basic"

  @skip:gke @skip:aks @skip:eks
  Scenario: Manage order event schema (JSON)
    Given there is no schema "order-event" in cluster "basic"
    When I apply Kubernetes manifest:
    """
# tag::order-event-json-schema-manifest[]
    # This manifest creates a JSON schema named "order-event" in the "basic" cluster.
    # The schema requires an "order_id" (string) and a "total" (number) field, with no additional properties allowed.
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Schema
    metadata:
      name: order-event
    spec:
      cluster:
        clusterRef:
          name: basic
      schemaType: json
      compatibilityLevel: None
      text: |
        {
          "$schema": "http://json-schema.org/draft-07/schema#",
          "type": "object",
          "properties": {
            "order_id": { "type": "string" },
            "total": { "type": "number" }
          },
          "required": ["order_id", "total"],
          "additionalProperties": false
        }
# end::order-event-json-schema-manifest[]
    """
    And schema "order-event" is successfully synced
    Then I should be able to check compatibility against "order-event" in cluster "basic"

  @skip:gke @skip:aks @skip:eks
  Scenario: Manage fully compatible schema (Avro)
    Given there is no schema "fully-compatible-schema" in cluster "basic"
    When I apply Kubernetes manifest:
    """
# tag::full-compatibility-schema-manifest[]
    # This manifest creates an Avro schema named "fully-compatible-schema" in the "basic" cluster.
    # The schema uses Full compatibility, ensuring backward and forward compatibility across versions.
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Schema
    metadata:
      name: fully-compatible-schema
      namespace: ${NAMESPACE}
    spec:
      cluster:
        clusterRef:
          name: basic
      schemaType: avro
      compatibilityLevel: Full
      text: |
        {
          "type": "record",
          "name": "ExampleRecord",
          "fields": [
            { "type": "string", "name": "field1" },
            { "type": "int", "name": "field2" }
          ]
        }
# end::full-compatibility-schema-manifest[]
    """
    And schema "fully-compatible-schema" is successfully synced
    Then I should be able to check compatibility against "fully-compatible-schema" in cluster "basic"

  @skip:gke @skip:aks @skip:eks
  Scenario: Manage product schema (Avro)
    Given there is no schema "product-schema" in cluster "basic"
    When I apply Kubernetes manifest:
    """
    # This manifest creates an Avro schema named "product-schema" in the "basic" cluster.
    # This schema will be referenced by other schemas to demonstrate schema references.
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Schema
    metadata:
      name: product-schema
      namespace: ${NAMESPACE}
    spec:
      cluster:
        clusterRef:
          name: basic
      schemaType: avro
      compatibilityLevel: Backward
      text: |
        {
          "type": "record",
          "name": "Product",
          "fields": [
            { "type": "string", "name": "product_id" },
            { "type": "string", "name": "name" }
          ]
        }
    """
    And schema "product-schema" is successfully synced
    Then I should be able to check compatibility against "product-schema" in cluster "basic"

  @skip:gke @skip:aks @skip:eks
  Scenario: Manage order schema with references (Avro)
    Given there is no schema "product-schema" in cluster "basic"
    And there is no schema "order-schema" in cluster "basic"
    When I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Schema
    metadata:
      name: product-schema
      namespace: ${NAMESPACE}
    spec:
      cluster:
        clusterRef:
          name: basic
      schemaType: avro
      compatibilityLevel: Backward
      text: |
        {
          "type": "record",
          "name": "Product",
          "fields": [
            { "type": "string", "name": "product_id" },
            { "type": "string", "name": "name" }
          ]
        }
    """
    And schema "product-schema" is successfully synced
    And there is a schema "product-schema" in cluster "basic"
    When I apply Kubernetes manifest:
    """
# tag::schema-references-manifest[]
    # This manifest creates an Avro schema named "order-schema" that references another schema.
    # Schema references enable modular and reusable schema components for complex data structures.
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Schema
    metadata:
      name: order-schema
      namespace: ${NAMESPACE}
    spec:
      cluster:
        clusterRef:
          name: basic
      references:
        - name: Product
          subject: product-schema
          version: 1
      text: |
        {
          "type": "record",
          "name": "Order",
          "fields": [
            { "name": "product", "type": "Product" }
          ]
        }
# end::schema-references-manifest[]
    """
    And schema "order-schema" is successfully synced
    Then I should be able to check compatibility against "order-schema" in cluster "basic"
