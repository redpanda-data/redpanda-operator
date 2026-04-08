@cluster:basic
Feature: Pipeline CRDs
  Background: Cluster available
    Given cluster "basic" is available

  Scenario: Create and run a Pipeline
    When I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Pipeline
    metadata:
      name: demo-pipeline
    spec:
      configYaml: |
        input:
          generate:
            mapping: 'root.message = "hello world"'
            interval: "5s"
        output:
          stdout: {}
      replicas: 1
    """
    Then pipeline "demo-pipeline" is successfully running

  Scenario: Pause and resume a Pipeline
    When I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Pipeline
    metadata:
      name: pausable-pipeline
    spec:
      configYaml: |
        input:
          generate:
            mapping: 'root.message = "hello"'
            interval: "5s"
        output:
          stdout: {}
      replicas: 1
    """
    And pipeline "pausable-pipeline" is successfully running
    When I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Pipeline
    metadata:
      name: pausable-pipeline
    spec:
      configYaml: |
        input:
          generate:
            mapping: 'root.message = "hello"'
            interval: "5s"
        output:
          stdout: {}
      replicas: 1
      paused: true
    """
    Then pipeline "pausable-pipeline" is stopped
