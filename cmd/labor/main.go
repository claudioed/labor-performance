// Command labor is the composition root for the Labor Performance
// service: it wires config from the environment to adapters, use cases,
// the HTTP router, and the inbound Kafka consumer, then serves both.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"

	inboundhttp "github.com/claudioed/labor-performance/internal/adapters/inbound/http"
	inboundkafka "github.com/claudioed/labor-performance/internal/adapters/inbound/kafka"
	"github.com/claudioed/labor-performance/internal/adapters/kafka/envelope"
	"github.com/claudioed/labor-performance/internal/adapters/outbound/events"
	outboundkafka "github.com/claudioed/labor-performance/internal/adapters/outbound/kafka"
	"github.com/claudioed/labor-performance/internal/adapters/outbound/memory"
	"github.com/claudioed/labor-performance/internal/adapters/outbound/postgres"
	"github.com/claudioed/labor-performance/internal/adapters/outbound/telemetry"
	"github.com/claudioed/labor-performance/internal/application/ports"
	"github.com/claudioed/labor-performance/internal/application/usecases"
)

// version is the service version reported as the OTel `service.version`
// resource attribute. Overridable at build time
// (-ldflags "-X main.version=1.2.3"), else SERVICE_VERSION, else "dev".
var version = ""

func main() {
	if err := run(); err != nil {
		slog.Error("service exited with error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	logger := newLogger(getenv("LOG_LEVEL", "info"))
	slog.SetDefault(logger)

	ctx := context.Background()

	serviceName := getenv("OTEL_SERVICE_NAME", inboundhttp.DefaultServiceName)
	otlpEndpoint := getenv("OTEL_EXPORTER_OTLP_ENDPOINT", telemetry.DefaultOTLPEndpoint)
	shutdownTelemetry, err := telemetry.Setup(ctx, serviceName, serviceVersion(), otlpEndpoint)
	if err != nil {
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownTelemetry(shutdownCtx); err != nil {
			logger.Warn("telemetry shutdown did not flush cleanly", "error", err)
		}
	}()
	logger.Info("telemetry configured",
		"service_name", serviceName,
		"service_version", serviceVersion(),
		"environment", telemetry.Environment(),
		"otlp_endpoint", otlpEndpoint,
	)

	httpAddr := getenv("HTTP_ADDR", ":8080")
	databaseURL := os.Getenv("DATABASE_URL")
	migrationsPath := getenv("MIGRATIONS_PATH", "migrations")

	standards, performances, processed, closeAdapters, err := buildRepoAdapters(ctx, databaseURL, migrationsPath, logger)
	if err != nil {
		return err
	}
	defer closeAdapters()

	publisher, closePublisher := buildEventPublisher(logger)
	defer closePublisher()
	clock := memory.SystemClock{}

	standardMetrics, err := telemetry.NewStandardMetrics()
	if err != nil {
		return err
	}

	recordTaskPerformance := &usecases.RecordTaskPerformance{
		Performances: performances,
		Standards:    standards,
		Processed:    processed,
		Events:       publisher,
		Clock:        clock,
	}

	server := &inboundhttp.Server{
		DefineStandard:         &usecases.DefineStandard{Standards: standards, Events: publisher, Clock: clock, Metrics: standardMetrics},
		GetStandard:            &usecases.GetStandard{Standards: standards},
		GetAssociateScorecard:  &usecases.GetAssociateScorecard{Performances: performances},
		GetTaskTypePerformance: &usecases.GetTaskTypePerformance{Performances: performances},
	}

	httpServer := &http.Server{
		Addr:              httpAddr,
		Handler:           inboundhttp.NewRouter(server, logger, serviceName),
		ReadHeaderTimeout: 5 * time.Second,
	}

	kafkaBrokers := strings.Split(getenv("KAFKA_BROKERS", "localhost:9092"), ",")
	kafkaGroupID := getenv("KAFKA_CONSUMER_GROUP", "labor-performance")
	consumer := inboundkafka.NewConsumer(kafkaBrokers, kafkaGroupID, recordTaskPerformance, logger)
	defer func() {
		if err := consumer.Close(); err != nil {
			logger.Error("error closing kafka consumer", "error", err)
		}
	}()

	consumerCtx, cancelConsumer := context.WithCancel(ctx)
	defer cancelConsumer()

	errCh := make(chan error, 2)
	go func() {
		logger.Info("http server listening", "addr", httpAddr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	go func() {
		logger.Info("kafka consumer starting", "brokers", kafkaBrokers, "group_id", kafkaGroupID, "topic", "warehouse.fulfillment.events")
		if err := consumer.Run(consumerCtx); err != nil {
			errCh <- err
		}
	}()

	stopCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-errCh:
		return err
	case <-stopCtx.Done():
	}

	cancelConsumer()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return httpServer.Shutdown(shutdownCtx)
}

// newLogger builds the process-wide structured logger, wrapped so any
// *Context log call made while a span is active carries trace_id/span_id.
func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(telemetry.NewTraceHandler(
		slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}),
	))
}

// serviceVersion resolves service.version: build-time ldflags first, then
// SERVICE_VERSION, then "dev".
func serviceVersion() string {
	if version != "" {
		return version
	}
	return getenv("SERVICE_VERSION", "dev")
}

// buildEventPublisher wires the outbound event publisher, returning it
// and a close function.
//
// The default is the log publisher this service has always used, so
// nothing about the existing OLTP behaviour changes unless it is opted
// into. Setting EVENT_PUBLISHER=kafka additionally fans every domain
// event onto warehouse.labor-performance.analytics, which is what feeds
// the analytical data product's projector (ADR-0007). The log publisher
// stays FIRST in the fan-out so a broker outage still leaves the event
// visible in the logs before the publish error surfaces.
//
// This is the only change the analytics data product makes to the OLTP
// composition root, and it makes none at all to the domain or
// application layers: they still see one ports.EventPublisher.
func buildEventPublisher(logger *slog.Logger) (ports.EventPublisher, func()) {
	logPublisher := events.NewLogPublisher(logger)
	if !strings.EqualFold(getenv("EVENT_PUBLISHER", "log"), "kafka") {
		return logPublisher, func() {}
	}

	brokers := strings.Split(getenv("KAFKA_BROKERS", "localhost:9092"), ",")
	analytics := outboundkafka.NewAnalyticsPublisher(brokers, uuid.NewString)
	logger.Info("analytics event publishing enabled",
		"topic", envelope.TopicLaborPerformanceAnalytics, "brokers", brokers)

	return outboundkafka.NewFanOutPublisher(logPublisher, analytics), func() {
		if err := analytics.Close(); err != nil {
			logger.Error("error closing analytics kafka publisher", "error", err)
		}
	}
}

// buildRepoAdapters wires the Postgres adapters when DATABASE_URL is set,
// or falls back to the in-memory adapters for local development without a
// database.
func buildRepoAdapters(ctx context.Context, databaseURL, migrationsPath string, logger *slog.Logger) (
	ports.StandardRepo, ports.PerformanceRepo, ports.ProcessedEvents, func(), error,
) {
	noop := func() {}

	if databaseURL == "" {
		logger.Info("database url not configured; using in-memory adapters")
		return memory.NewStandardRepo(), memory.NewPerformanceRepo(), memory.NewProcessedEventRepo(), noop, nil
	}

	if err := postgres.RunMigrations(databaseURL, migrationsPath); err != nil {
		return nil, nil, nil, noop, err
	}
	pool, err := postgres.NewPool(ctx, databaseURL)
	if err != nil {
		return nil, nil, nil, noop, err
	}
	return postgres.NewStandardRepo(pool), postgres.NewPerformanceRepo(pool), postgres.NewProcessedEventRepo(pool), pool.Close, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
