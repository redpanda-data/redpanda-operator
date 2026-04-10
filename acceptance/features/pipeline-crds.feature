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

  Scenario: Pipeline produces to Redpanda via clusterRef
    When I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Pipeline
    metadata:
      name: producer-pipeline
    spec:
      cluster:
        clusterRef:
          name: basic
      configYaml: |
        input:
          generate:
            count: 0
            interval: "1s"
            mapping: 'root.message = "hello from pipeline"'
        output:
          redpanda:
            seed_brokers:
              - "${RPK_BROKERS}"
            tls:
              enabled: ${RPK_TLS_ENABLED}
              root_cas_file: "${RPK_TLS_ROOT_CAS_FILE}"
            topic: "pipeline-produce-test"
      replicas: 1
    """
    Then pipeline "producer-pipeline" is successfully running
    And topic "pipeline-produce-test" has messages in cluster "basic"

  Scenario: Pipeline reads from Redpanda via clusterRef
    Given I create topic "pipeline-consume-test" in cluster "basic"
    And I produce messages to "pipeline-consume-test" in cluster "basic"
    When I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Pipeline
    metadata:
      name: consumer-pipeline
    spec:
      cluster:
        clusterRef:
          name: basic
      configYaml: |
        input:
          redpanda:
            seed_brokers:
              - "${RPK_BROKERS}"
            tls:
              enabled: ${RPK_TLS_ENABLED}
              root_cas_file: "${RPK_TLS_ROOT_CAS_FILE}"
            topics:
              - "pipeline-consume-test"
            consumer_group: "pipeline-consumer-group"
        output:
          drop: {}
      replicas: 1
    """
    Then pipeline "consumer-pipeline" is successfully running
