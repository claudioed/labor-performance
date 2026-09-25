package http_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

type standardBodyWithTravel struct {
	TaskType               string  `json:"taskType"`
	ExpectedSeconds        int64   `json:"expectedSeconds"`
	TravelComponentSeconds *int64  `json:"travelComponentSeconds"`
	EffectiveFrom          string  `json:"effectiveFrom"`
	EffectiveTo            *string `json:"effectiveTo"`
}

// TestPostStandards_WithTravelComponentSeconds_IsEchoedInResponse proves
// the optional field round-trips through the REST surface (ADR 0015).
func TestPostStandards_WithTravelComponentSeconds_IsEchoedInResponse(t *testing.T) {
	e := newTestEnv(t, now)
	rec := e.do(t, http.MethodPost, "/standards", `{"taskType":"PICK","expectedSeconds":45,"travelComponentSeconds":15}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body: %s)", rec.Code, rec.Body.String())
	}
	var body standardBodyWithTravel
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.TravelComponentSeconds == nil || *body.TravelComponentSeconds != 15 {
		t.Fatalf("body.TravelComponentSeconds = %v, want 15", body.TravelComponentSeconds)
	}
}

// TestPostStandards_NoTravelComponentSeconds_OmitsFieldFromResponse
// proves the field is omitted entirely (not present with a null/zero
// value) when the caller never declared one -- the default, and the only
// state that existed before this feature.
func TestPostStandards_NoTravelComponentSeconds_OmitsFieldFromResponse(t *testing.T) {
	e := newTestEnv(t, now)
	rec := e.do(t, http.MethodPost, "/standards", `{"taskType":"PACK","expectedSeconds":60}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body: %s)", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, present := resp["travelComponentSeconds"]; present {
		t.Fatalf("expected travelComponentSeconds to be omitted, got %+v", resp)
	}
}

// TestPostStandards_NegativeTravelComponentSeconds_Returns422 proves the
// domain's own guard-rail surfaces as a 422 RFC 7807 problem, not a
// silently-accepted or fabricated value.
func TestPostStandards_NegativeTravelComponentSeconds_Returns422(t *testing.T) {
	e := newTestEnv(t, now)
	rec := e.do(t, http.MethodPost, "/standards", `{"taskType":"PICK","expectedSeconds":45,"travelComponentSeconds":-1}`)
	p := assertProblem(t, rec, http.StatusUnprocessableEntity)
	if !strings.HasSuffix(p.Type, "negative-travel-component-seconds") {
		t.Fatalf("problem.type = %q", p.Type)
	}
}

// TestPostStandards_TravelComponentSecondsExceedsExpectedSeconds_Returns422
// proves the "travel component can't exceed the whole standard" guard-
// rail is enforced over HTTP too.
func TestPostStandards_TravelComponentSecondsExceedsExpectedSeconds_Returns422(t *testing.T) {
	e := newTestEnv(t, now)
	rec := e.do(t, http.MethodPost, "/standards", `{"taskType":"PICK","expectedSeconds":45,"travelComponentSeconds":46}`)
	p := assertProblem(t, rec, http.StatusUnprocessableEntity)
	if !strings.HasSuffix(p.Type, "travel-component-exceeds-expected-seconds") {
		t.Fatalf("problem.type = %q", p.Type)
	}
}

// TestGetStandard_ReturnsTravelComponentSeconds proves GetStandard's
// response carries the same field GetStandard reads back after a
// definition that declared one.
func TestGetStandard_ReturnsTravelComponentSeconds(t *testing.T) {
	e := newTestEnv(t, now)
	defineRec := e.do(t, http.MethodPost, "/standards", `{"taskType":"SLAM","expectedSeconds":30,"travelComponentSeconds":5}`)
	if defineRec.Code != http.StatusCreated {
		t.Fatalf("setup: status = %d, want 201 (body: %s)", defineRec.Code, defineRec.Body.String())
	}

	getRec := e.do(t, http.MethodGet, "/standards/SLAM", "")
	if getRec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", getRec.Code, getRec.Body.String())
	}
	var body standardBodyWithTravel
	if err := json.NewDecoder(getRec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.TravelComponentSeconds == nil || *body.TravelComponentSeconds != 5 {
		t.Fatalf("body.TravelComponentSeconds = %v, want 5", body.TravelComponentSeconds)
	}
}
