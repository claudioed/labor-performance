package http

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/claudioed/labor-performance/internal/application/ports"
	"github.com/claudioed/labor-performance/internal/application/usecases"
	"github.com/claudioed/labor-performance/internal/domain/shared"
)

// DefaultServiceName labels this service in logs when the caller does not
// supply one.
const DefaultServiceName = "labor-performance"

// Server holds every use case the HTTP adapter depends on.
type Server struct {
	DefineStandard         *usecases.DefineStandard
	GetStandard            *usecases.GetStandard
	GetAssociateScorecard  *usecases.GetAssociateScorecard
	GetTaskTypePerformance *usecases.GetTaskTypePerformance
}

// NewRouter builds the chi router for every endpoint in SPEC.md's REST
// API. A nil logger defaults to slog.Default().
func NewRouter(s *Server, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(RequestLogger(logger))
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware())

	r.Get("/healthz", s.handleHealthz)
	r.Post("/standards", s.handleDefineStandard)
	r.Get("/standards/{taskType}", s.handleGetStandard)
	r.Get("/associates/{associateId}/scorecard", s.handleGetAssociateScorecard)
	r.Get("/task-types/{taskType}/performance", s.handleGetTaskTypePerformance)

	return r
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleDefineStandard(w http.ResponseWriter, r *http.Request) {
	var req defineStandardRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	taskType, err := shared.NewTaskType(req.TaskType)
	if err != nil {
		writeError(w, r, err)
		return
	}

	st, err := s.DefineStandard.Execute(r.Context(), taskType, req.ExpectedSeconds)
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, toStandardResponse(st.TaskType(), st.ExpectedSeconds(), st.EffectiveFrom(), st.EffectiveTo()))
}

func (s *Server) handleGetStandard(w http.ResponseWriter, r *http.Request) {
	taskType, err := shared.NewTaskType(chi.URLParam(r, "taskType"))
	if err != nil {
		writeError(w, r, err)
		return
	}

	st, err := s.GetStandard.Execute(r.Context(), taskType)
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, toStandardResponse(st.TaskType(), st.ExpectedSeconds(), st.EffectiveFrom(), st.EffectiveTo()))
}

func (s *Server) handleGetAssociateScorecard(w http.ResponseWriter, r *http.Request) {
	associateId := shared.AssociateId(chi.URLParam(r, "associateId"))

	sc, err := s.GetAssociateScorecard.Execute(r.Context(), associateId)
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, toScorecardResponse(sc))
}

func (s *Server) handleGetTaskTypePerformance(w http.ResponseWriter, r *http.Request) {
	taskType, err := shared.NewTaskType(chi.URLParam(r, "taskType"))
	if err != nil {
		writeError(w, r, err)
		return
	}

	tp, err := s.GetTaskTypePerformance.Execute(r.Context(), taskType)
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, taskTypePerformanceResponse{
		TaskType:          string(tp.TaskType),
		TaskCount:         tp.TaskCount,
		MeanEfficiencyPct: tp.MeanEfficiencyPct,
	})
}

const timeFormat = time.RFC3339

func toStandardResponse(taskType shared.TaskType, expectedSeconds int64, effectiveFrom time.Time, effectiveTo *time.Time) standardResponse {
	var to *string
	if effectiveTo != nil {
		formatted := effectiveTo.UTC().Format(timeFormat)
		to = &formatted
	}
	return standardResponse{
		TaskType:        string(taskType),
		ExpectedSeconds: expectedSeconds,
		EffectiveFrom:   effectiveFrom.UTC().Format(timeFormat),
		EffectiveTo:     to,
	}
}

func toScorecardResponse(sc ports.Scorecard) scorecardResponse {
	byType := make(map[string]taskTypeBreakdownResponse, len(sc.ByTaskType))
	for tt, b := range sc.ByTaskType {
		byType[string(tt)] = taskTypeBreakdownResponse{
			TaskCount:         b.TaskCount,
			MeanEfficiencyPct: b.MeanEfficiencyPct,
		}
	}
	return scorecardResponse{
		AssociateId:       string(sc.AssociateId),
		TaskCount:         sc.TaskCount,
		MeanEfficiencyPct: sc.MeanEfficiencyPct,
		ByTaskType:        byType,
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dest any) bool {
	if err := json.NewDecoder(r.Body).Decode(dest); err != nil {
		writeProblem(w, http.StatusBadRequest, problemInfo{"malformed-request-body", "The request body is not valid JSON"}, err.Error(), r.URL.Path)
		return false
	}
	return true
}

// writeError writes a domain/application error as an RFC 7807
// (application/problem+json) response.
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	writeProblem(w, statusFor(err), problemFor(err), err.Error(), r.URL.Path)
}

func writeProblem(w http.ResponseWriter, status int, info problemInfo, detail, instance string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(problemDetails{
		Type:     problemBaseURI + info.slug,
		Title:    info.title,
		Status:   status,
		Detail:   detail,
		Instance: instance,
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// corsMiddleware allows the warehouse-console browser SPA (and this
// service's own future labor-mfe remote dev origin) to call this API
// directly from the browser. CORS_ALLOWED_ORIGINS overrides the local-dev
// default (comma-separated) for staging/prod deployments. No labor-mfe
// screen exists yet in this round — see the README's "Deferred (v1)"
// section — but CORS ships now, matching the fleet's convention that CORS
// is added alongside a service's first console-facing REST surface.
func corsMiddleware() func(http.Handler) http.Handler {
	origins := []string{"http://localhost:5173", "http://localhost:5187"}
	if v := os.Getenv("CORS_ALLOWED_ORIGINS"); v != "" {
		origins = strings.Split(v, ",")
	}
	return cors.Handler(cors.Options{
		AllowedOrigins:   origins,
		AllowedMethods:   []string{http.MethodGet, http.MethodPost},
		AllowedHeaders:   []string{"Content-Type", "Authorization"},
		AllowCredentials: false,
		MaxAge:           300,
	})
}
