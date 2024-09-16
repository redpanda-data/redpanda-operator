@isolated
Feature: stub
  Scenario Outline: templated stub
    Given there is a stub
    When a user updates the stub key "<key>" to "<value>"
    Then the stub should have "<key>" equal "<value>"

    Examples:
        | key       | value |
        | stub      | true  |

  @isolated
  Scenario: isolated stub
    And there is a stub
    When a user updates the stub key "foo" to "bar"
    Then the stub should have "foo" equal "bar"
    And there is no error

  @skip:stub
  Scenario: skipped stub
    And there is a stub
    When a user updates the stub key "a" to "b"
    Then the stub should have "c" equal "d"
