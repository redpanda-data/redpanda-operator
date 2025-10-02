@cluster:vectorized/basic
Feature: Vectorized Schema CRDs
  Background: Vectorized Cluster available
    Given vectorized cluster "basic" is available

  @skip:gke @skip:aks @skip:eks
  Scenario: Manage customer profile vectorized schema (Avro)
    Given there is no schema "customer-profile" in vectorized cluster "basic"
    When I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Schema
    metadata:
      name: customer-profile
    spec:
      cluster:
        clusterRef:
          group: redpanda.vectorized.io
          kind: Cluster
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
    """
    And schema "customer-profile" is successfully synced
    Then I should be able to check compatibility against "customer-profile" in vectorized cluster "basic"

  @skip:gke @skip:aks @skip:eks
  Scenario: Manage product catalog vectorized schema (Protobuf)
    Given there is no schema "product-catalog" in vectorized cluster "basic"
    When I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Schema
    metadata:
      name: product-catalog
    spec:
      cluster:
        clusterRef:
          group: redpanda.vectorized.io
          kind: Cluster
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
    """
    And schema "product-catalog" is successfully synced
    Then I should be able to check compatibility against "product-catalog" in vectorized cluster "basic"

  @skip:gke @skip:aks @skip:eks
  Scenario: Manage order event vectorized schema (JSON)
    Given there is no schema "order-event" in vectorized cluster "basic"
    When I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Schema
    metadata:
      name: order-event
    spec:
      cluster:
        clusterRef:
          group: redpanda.vectorized.io
          kind: Cluster
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
    """
    And schema "order-event" is successfully synced
    Then I should be able to check compatibility against "order-event" in vectorized cluster "basic"
