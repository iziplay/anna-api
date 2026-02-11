package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	anna "github.com/iziplay/anna-api"
	routing "github.com/iziplay/anna-api/pkg/api"
	"github.com/iziplay/anna-api/pkg/database"
	"github.com/iziplay/anna-api/pkg/sync"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.39.0"
	"gorm.io/plugin/opentelemetry/tracing"
)

func init() {
	database.Ping()
}

func getLogLevelFromEnv() slog.Level {
	levelStr := os.Getenv("LOG_LEVEL")

	switch strings.ToLower(levelStr) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func main() {
	ctx := context.Background()
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: getLogLevelFromEnv()})))

	exp, err := otlptracegrpc.New(ctx)
	if err != nil {
		panic(err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(
			resource.NewWithAttributes(
				semconv.SchemaURL,
				semconv.ServiceName("anna-api"),
			),
		),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		),
	)

	database.DB.Use(tracing.NewPlugin())

	router := chi.NewRouter()

	router.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "HEAD", "PUT", "PATCH", "POST", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Origin", "Authorization", "Content-Type"},
		ExposedHeaders:   []string{"Server"},
		AllowCredentials: false,
	}))

	addr := ":80"
	if port, hasPort := os.LookupEnv("API_PORT"); hasPort {
		addr = ":" + port
	}

	host := "http://localhost"
	if hostEnv, hasHost := os.LookupEnv("API_HOST"); hasHost {
		host = hostEnv
	} else {
		host += addr
	}

	config := huma.DefaultConfig("Anna API", "1.0.0")
	config.OpenAPI.Info.Description = anna.Readme
	config.OpenAPI.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"bearerAuth": {
			Type:         "http",
			Scheme:       "bearer",
			BearerFormat: "JWT",
		},
	}
	config.DocsPath = "/"
	config.Servers = []*huma.Server{
		{URL: host},
	}
	api := humachi.New(router, config)

	routing.Setup(api)

	server := &http.Server{
		Addr:    addr,
		Handler: otelhttp.NewHandler(router, "api"),
	}

	go func() {
		slog.Info("Starting server", "addr", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Server failed", "error", err)
			os.Exit(1)
		}
	}()

	go database.ComputeAndCacheStats(false)

	for {
		ctx := context.Background()

		// Calculate time until next sync
		var sleepDuration time.Duration
		lastSync, err := sync.GetLastSync(ctx)
		if err != nil {
			slog.Error("Failed to get last sync", "error", err)
			os.Exit(1)
		} else {
			if lastSync != nil {
				sleepDuration = time.Until(lastSync.Date.Add(24 * time.Hour))
			}

			// If sleep duration is negative or very small, sync immediately
			if sleepDuration <= 0 {
				sleepDuration = 0
			}
		}

		slog.Info("Next sync scheduled", "in", sleepDuration)
		time.Sleep(sleepDuration)

		// Perform sync
		if err := sync.Sync(ctx); err != nil {
			slog.Error("Sync failed", "error", err)
		}
	}
}
