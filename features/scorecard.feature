Feature: Reading an associate's performance scorecard
  As the fleet's labor performance service
  I want to expose a 404 for an associate this service has never heard of
  So that "no data" is never confused with "a zero score"

  Background:
    Given the Labor Performance service is running

  @bdd
  Scenario: An associate with zero recorded TaskPerformance rows returns 404
    # RecordTaskPerformance is exclusively Kafka-consumer-driven (see
    # ADR 0003-kafka-choreography-consumer-of-fulfillment-execution.md) —
    # there is deliberately no REST endpoint that ever writes a
    # TaskPerformance row, so no Given step in this suite can put one on
    # the wire over HTTP. This scenario therefore covers exactly what a
    # pure REST-only BDD suite CAN assert for this endpoint: a truly
    # unknown associate id 404s. The "associate has 1+ recorded rows"
    # scenario (200 with a real scorecard, including the "has rows but
    # all-nil efficiency still returns 200" distinction) is deliberately
    # NOT covered here — it is already covered by
    # get_associate_scorecard's own unit tests and by
    # RecordTaskPerformance's Kafka-consumer integration test, which are
    # the layers that can actually establish that precondition. See this
    # PR's summary for the judgment call and why no repo in this fleet's
    # BDD suites calls a use case directly from a Given step to bypass
    # this kind of REST-unreachable precondition.
    When the scorecard for associate "assoc-never-seen" is requested
    Then the request is rejected with status 404
