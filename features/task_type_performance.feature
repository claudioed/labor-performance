Feature: Reading fleet-wide TaskType performance
  As the fleet's labor performance service
  I want GetTaskTypePerformance to always return 200
  So that a TaskType this service has never recorded is a real (zero-count) result, not a "not found"

  Background:
    Given the Labor Performance service is running

  @bdd
  Scenario: A TaskType never recorded still returns a 200 zero-count result
    When the fleet-wide performance for task type "SLAM" is requested
    Then the request is accepted with status 200
    And the task type performance response reports task type "SLAM" and task count 0

  @bdd
  Scenario: Requesting fleet-wide performance for an unrecognized TaskType is rejected
    When the fleet-wide performance for task type "UNLOAD" is requested
    Then the request is rejected with status 400
