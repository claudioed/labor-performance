Feature: Defining and reading engineered labor standards
  As the fleet's labor performance service
  I want to define and revise engineered labor standards per TaskType
  So that a completed task can later be scored against the standard genuinely active when it finished

  Background:
    Given the Labor Performance service is running

  @bdd
  Scenario: Defining a standard for a TaskType succeeds
    When a standard of 45 expected seconds is defined for task type "PICK"
    Then the request is accepted with status 201
    And the standard response reports task type "PICK" and expected seconds 45

  @bdd
  Scenario: Revising an already-active standard closes the prior one and starts a new one
    Given a standard of 45 expected seconds is already defined for task type "PACK"
    When a standard of 40 expected seconds is defined for task type "PACK"
    Then the request is accepted with status 201
    And the standard response reports task type "PACK" and expected seconds 40
    When the currently-active standard for task type "PACK" is requested
    Then the request is accepted with status 200
    And the standard response reports task type "PACK" and expected seconds 40

  @bdd
  Scenario: Getting a standard for a TaskType with no active standard returns 404
    When the currently-active standard for task type "SLAM" is requested
    Then the request is rejected with status 404

  @bdd
  Scenario: Defining a standard with a non-positive expected duration is rejected
    When a standard of 0 expected seconds is defined for task type "PICK"
    Then the request is rejected with status 422

  @bdd
  Scenario: Getting a standard for an unrecognized TaskType is rejected
    When the currently-active standard for task type "UNLOAD" is requested
    Then the request is rejected with status 400
