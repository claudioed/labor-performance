# Makefile — the local quality gate for labor-performance.
#
# Every target below mirrors a sensor in .github/workflows/ci.yml, so the
# same feedback CI gives you post-push is available locally, pre-commit.
# See CLAUDE.md's "Local quality gate" section.
#
# v1 deliberately has NO mutation/bdd targets — matching order-management's
# own v1 scope-cut — but DOES include a Kafka integration test target,
# since the consumer is this service's core integration surface and
# deserves real verification against a broker, unlike order-management v1
# which had no async integration at all.

GO                 ?= go
GOLANGCI_LINT      ?= golangci-lint
GOLANGCI_VERSION   := v2.13.1

COVERAGE_OUT       := coverage.out
# internal/analytics is inside the gate alongside domain/application: it is
# the analytical READ MODEL (ADR 0007) — pure, dependency-free aggregation
# logic that decides whether a mean exists at all, which is exactly the
# kind of code the gate is for. Keep this list in sync with ci.yml's
# -coverpkg.
COVERAGE_PKGS      := ./internal/domain/...,./internal/application/...,./internal/analytics/...
COVERAGE_THRESHOLD := 90

.DEFAULT_GOAL := help

.PHONY: help build vet fmt fmt-check lint test coverage check check-all integration-kafka

help:
	@echo "labor-performance — local quality gate (targets mirror .github/workflows/ci.yml)"
	@echo ""
	@echo "  help              Print this list of targets (default target)"
	@echo "  build             go build ./..."
	@echo "  vet               go vet ./..."
	@echo "  fmt               gofmt -w . — format the tree in place"
	@echo "  fmt-check         Fail if gofmt -l . is non-empty (the CI-style check)"
	@echo "  lint              golangci-lint run ./... (pinned $(GOLANGCI_VERSION) in CI)"
	@echo "  test              go test ./... -race — unit + httptest + fake-reader kafka, no broker/DB needed"
	@echo "  coverage          CI coverage command + the $(COVERAGE_THRESHOLD)% gate"
	@echo "  integration-kafka Build-tagged Kafka consumer test against a real broker (needs KAFKA_BROKERS)"
	@echo ""
	@echo "  check             FAST bundle: fmt-check vet build lint test"
	@echo "  check-all         check + coverage — run this before pushing"

build:
	$(GO) build ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -w .

fmt-check:
	@files=$$(gofmt -l .); \
	if [ -n "$$files" ]; then \
		echo "gofmt: the following files are not formatted:"; \
		echo "$$files" | sed 's/^/  /'; \
		echo "run 'make fmt' to fix them"; \
		exit 1; \
	fi; \
	echo "gofmt: clean"

lint:
	@if ! command -v $(GOLANGCI_LINT) >/dev/null 2>&1; then \
		echo "golangci-lint is not installed (or not on PATH)."; \
		echo "Install the exact version CI pins:"; \
		echo "  go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)"; \
		exit 1; \
	fi
	$(GOLANGCI_LINT) run ./...

test:
	$(GO) test ./... -race

coverage:
	$(GO) test ./... -race -coverprofile=$(COVERAGE_OUT) -coverpkg=$(COVERAGE_PKGS)
	@COVERAGE=$$($(GO) tool cover -func=$(COVERAGE_OUT) | awk '/^total:/ {print $$3}' | tr -d '%'); \
	echo "Coverage: $${COVERAGE}% (gate: $(COVERAGE_THRESHOLD)%)"; \
	if awk -v c="$$COVERAGE" -v t="$(COVERAGE_THRESHOLD)" 'BEGIN { exit !(c < t) }'; then \
		echo "coverage $${COVERAGE}% is below the $(COVERAGE_THRESHOLD)% gate"; \
		exit 1; \
	fi

# Requires a running broker: docker compose -f docker-compose.kafka.yml up -d
# (or the fleet's shared broker) and KAFKA_BROKERS set. Skipped without it.
integration-kafka:
	$(GO) test -tags=integration ./internal/adapters/inbound/kafka/... -run TestConsumer_ProjectsRealBrokerMessages -v

# The fast self-correction loop: run this after every change, before committing.
check: fmt-check vet build lint test

# The fuller gate a human runs before pushing.
check-all: check coverage
