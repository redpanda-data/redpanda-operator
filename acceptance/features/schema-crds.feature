@cluster:basic
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
