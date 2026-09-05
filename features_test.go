// Package main_test contains the BDD / acceptance test suite: godog
// (Cucumber for Go) drives the Gherkin scenarios under features/ against
// the real chi router over HTTP, wired to the same in-memory adapters the
// service's own httptest suite uses. It is a black-box test — it only
// ever touches the REST API.
//
// Scope note: this suite covers the REST-visible behavior only (define/
// revise/get a standard, get-scorecard, get-task-type-performance). The
// Kafka-consumer-driven RecordTaskPerformance path is NOT REST-reachable,
// so it is NOT BDD material here — it already has its own unit
// (internal/application/usecases) and integration
// (internal/adapters/inbound/kafka/consumer_integration_test.go) test
// coverage. This mirrors what wes-work-planning's suite does for ITS REST
// contract.
package main_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cucumber/godog"

	inboundhttp "github.com/claudioed/labor-performance/internal/adapters/inbound/http"
	"github.com/claudioed/labor-performance/internal/adapters/outbound/events"
	"github.com/claudioed/labor-performance/internal/adapters/outbound/memory"
	"github.com/claudioed/labor-performance/internal/application/usecases"
)

// newServer builds the production router over fresh in-memory adapters and
// serves it from an httptest server, mirroring the wiring in
// internal/adapters/inbound/http/server_test.go.
func newServer() *httptest.Server {
	standards := memory.NewStandardRepo()
	performances := memory.NewPerformanceRepo()
	publisher := events.NewLogPublisher(nil)
	clock := memory.SystemClock{}

	s := &inboundhttp.Server{
		DefineStandard:         &usecases.DefineStandard{Standards: standards, Events: publisher, Clock: clock},
		GetStandard:            &usecases.GetStandard{Standards: standards},
		GetAssociateScorecard:  &usecases.GetAssociateScorecard{Performances: performances},
		GetTaskTypePerformance: &usecases.GetTaskTypePerformance{Performances: performances},
	}

	return httptest.NewServer(inboundhttp.NewRouter(s, nil, ""))
}

// world is the per-scenario state: one server with its own in-memory
// adapters, plus the last HTTP response the steps made.
type world struct {
	server *httptest.Server

	lastStatus int
	lastBody   []byte
}

func (w *world) reset() {
	if w.server != nil {
		w.server.Close()
	}
	w.server = newServer()
	w.lastStatus = 0
	w.lastBody = nil
}

func (w *world) close() {
	if w.server != nil {
		w.server.Close()
		w.server = nil
	}
}

// do performs a real net/http call against the httptest server and records
// the response as the "last" one for the assertion steps.
func (w *world) do(method, path string, body any) error {
	var reader io.Reader = http.NoBody
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(context.Background(), method, w.server.URL+path, reader)
	if err != nil {
		return fmt.Errorf("build %s %s: %w", method, path, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := w.server.Client().Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read %s %s response: %w", method, path, err)
	}

	w.lastStatus = resp.StatusCode
	w.lastBody = raw
	return nil
}

// decodeLast unmarshals the last response body into a generic JSON object.
func (w *world) decodeLast() (map[string]any, error) {
	var out map[string]any
	if err := json.Unmarshal(w.lastBody, &out); err != nil {
		return nil, fmt.Errorf("decode response %q: %w", string(w.lastBody), err)
	}
	return out, nil
}

func (w *world) stringField(field string) (string, error) {
	obj, err := w.decodeLast()
	if err != nil {
		return "", err
	}
	value, ok := obj[field].(string)
	if !ok {
		return "", fmt.Errorf("response has no string field %q: %s", field, string(w.lastBody))
	}
	return value, nil
}

func (w *world) numberField(field string) (float64, error) {
	obj, err := w.decodeLast()
	if err != nil {
		return 0, err
	}
	value, ok := obj[field].(float64)
	if !ok {
		return 0, fmt.Errorf("response has no numeric field %q: %s", field, string(w.lastBody))
	}
	return value, nil
}

// --- Given steps -------------------------------------------------------

func (w *world) serviceIsRunning() error {
	if err := w.do(http.MethodGet, "/healthz", nil); err != nil {
		return err
	}
	if w.lastStatus != http.StatusOK {
		return fmt.Errorf("healthz returned %d, want 200", w.lastStatus)
	}
	return nil
}

func (w *world) standardDefined(expectedSeconds int, taskType string) error {
	if err := w.defineStandard(expectedSeconds, taskType); err != nil {
		return err
	}
	if w.lastStatus != http.StatusCreated {
		return fmt.Errorf("define standard returned %d, want 201: %s", w.lastStatus, string(w.lastBody))
	}
	return nil
}

// --- When steps ----------------------------------------------------------

func (w *world) defineStandard(expectedSeconds int, taskType string) error {
	return w.do(http.MethodPost, "/standards", map[string]any{
		"taskType":        taskType,
		"expectedSeconds": expectedSeconds,
	})
}

func (w *world) getStandard(taskType string) error {
	return w.do(http.MethodGet, "/standards/"+taskType, nil)
}

func (w *world) getScorecard(associateId string) error {
	return w.do(http.MethodGet, "/associates/"+associateId+"/scorecard", nil)
}

func (w *world) getTaskTypePerformance(taskType string) error {
	return w.do(http.MethodGet, "/task-types/"+taskType+"/performance", nil)
}

// --- Then steps ----------------------------------------------------------

func (w *world) requestAccepted(status int) error {
	if w.lastStatus != status {
		return fmt.Errorf("got status %d, want %d: %s", w.lastStatus, status, string(w.lastBody))
	}
	return nil
}

func (w *world) standardResponseReports(taskType string, expectedSeconds int) error {
	gotType, err := w.stringField("taskType")
	if err != nil {
		return err
	}
	if gotType != taskType {
		return fmt.Errorf("got task type %q, want %q", gotType, taskType)
	}
	gotSeconds, err := w.numberField("expectedSeconds")
	if err != nil {
		return err
	}
	if int(gotSeconds) != expectedSeconds {
		return fmt.Errorf("got expected seconds %d, want %d", int(gotSeconds), expectedSeconds)
	}
	return nil
}

func (w *world) taskTypePerformanceResponseReports(taskType string, taskCount int) error {
	gotType, err := w.stringField("taskType")
	if err != nil {
		return err
	}
	if gotType != taskType {
		return fmt.Errorf("got task type %q, want %q", gotType, taskType)
	}
	gotCount, err := w.numberField("taskCount")
	if err != nil {
		return err
	}
	if int(gotCount) != taskCount {
		return fmt.Errorf("got task count %d, want %d", int(gotCount), taskCount)
	}
	return nil
}

// InitializeScenario registers every step definition and gives each
// scenario a fresh server over fresh in-memory adapters, so scenarios are
// independent.
func InitializeScenario(sc *godog.ScenarioContext) {
	w := &world{}

	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		w.reset()
		return ctx, nil
	})
	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w.close()
		return ctx, nil
	})

	sc.Step(`^the Labor Performance service is running$`, w.serviceIsRunning)

	sc.Step(`^a standard of (\d+) expected seconds is already defined for task type "([^"]*)"$`, w.standardDefined)

	sc.Step(`^a standard of (\d+) expected seconds is defined for task type "([^"]*)"$`, w.defineStandard)
	sc.Step(`^the currently-active standard for task type "([^"]*)" is requested$`, w.getStandard)
	sc.Step(`^the scorecard for associate "([^"]*)" is requested$`, w.getScorecard)
	sc.Step(`^the fleet-wide performance for task type "([^"]*)" is requested$`, w.getTaskTypePerformance)

	sc.Step(`^the request is accepted with status (\d+)$`, w.requestAccepted)
	sc.Step(`^the request is rejected with status (\d+)$`, w.requestAccepted)
	sc.Step(`^the standard response reports task type "([^"]*)" and expected seconds (\d+)$`, w.standardResponseReports)
	sc.Step(`^the task type performance response reports task type "([^"]*)" and task count (\d+)$`, w.taskTypePerformanceResponseReports)
}

// TestFeatures runs the Gherkin acceptance suite under features/.
func TestFeatures(t *testing.T) {
	suite := godog.TestSuite{
		ScenarioInitializer: InitializeScenario,
		Options: &godog.Options{
			Format: "pretty",
			Paths:  []string{"features"},
			// Strict makes an undefined or pending step fail the suite
			// instead of silently skipping it.
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("non-zero status returned, failed to run feature tests")
	}
}
