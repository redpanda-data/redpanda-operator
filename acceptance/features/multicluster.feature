@operator:none
Feature: Multicluster Operator

  Scenario: Multicluster reconciliation
    Given I create a multicluster operator with 3 nodes
    Then I become debuggable
