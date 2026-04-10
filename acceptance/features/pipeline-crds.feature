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

  Scenario: Delete a Pipeline
    When I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Pipeline
    metadata:
      name: delete-pipeline
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
    And pipeline "delete-pipeline" is successfully running
    When I delete the CRD pipeline "delete-pipeline"
    Then pipeline "delete-pipeline" does not exist

  Scenario: Update a Pipeline config
    When I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Pipeline
    metadata:
      name: update-pipeline
    spec:
      configYaml: |
        input:
          generate:
            mapping: 'root.message = "original"'
            interval: "5s"
        output:
          stdout: {}
      replicas: 1
    """
    And pipeline "update-pipeline" is successfully running
    When I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Pipeline
    metadata:
      name: update-pipeline
    spec:
      configYaml: |
        input:
          generate:
            mapping: 'root.message = "updated"'
            interval: "5s"
        output:
          stdout: {}
      replicas: 1
    """
    Then pipeline "update-pipeline" is successfully running

  Scenario: Stop a Pipeline
    When I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Pipeline
    metadata:
      name: stop-pipeline
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
    And pipeline "stop-pipeline" is successfully running
    When I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Pipeline
    metadata:
      name: stop-pipeline
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
    Then pipeline "stop-pipeline" is stopped

  Scenario: Resume a stopped Pipeline
    When I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Pipeline
    metadata:
      name: resume-pipeline
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
    And pipeline "resume-pipeline" is successfully running
    When I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Pipeline
    metadata:
      name: resume-pipeline
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
    And pipeline "resume-pipeline" is stopped
    When I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Pipeline
    metadata:
      name: resume-pipeline
    spec:
      configYaml: |
        input:
          generate:
            mapping: 'root.message = "hello"'
            interval: "5s"
        output:
          stdout: {}
      replicas: 1
      paused: false
    """
    Then pipeline "resume-pipeline" is successfully running

  Scenario: Invalid Pipeline config detected by lint
    When I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Pipeline
    metadata:
      name: invalid-pipeline
    spec:
      configYaml: |
        input:
          not_a_real_input:
            mapping: 'root = "broken"'
        output:
          stdout: {}
      replicas: 1
    """
    Then pipeline "invalid-pipeline" has invalid config
