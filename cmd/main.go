package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	routing "github.com/iziplay/anna-api/api"
	"github.com/iziplay/anna-api/pkg/database"
	"github.com/iziplay/anna-api/pkg/sync"
)

func init() {
	database.Ping()
}

func main() {
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
	config.Servers = []*huma.Server{
		{URL: host},
	}
	api := humachi.New(router, config)

	routing.Setup(api)

	server := &http.Server{
		Addr:    addr,
		Handler: router,
	}

	go func() {
		log.Printf("Starting server on %s", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	go database.ComputeAndCacheStats(false)

	for {
		// Calculate time until next sync
		var sleepDuration time.Duration
		lastSync, err := sync.GetLastSync()
		if err != nil {
			// If no last sync: the first sync was errored, wait 24 hours
			sleepDuration = 24 * time.Hour
		} else {
			sleepDuration = time.Until(lastSync.Date.Add(24 * time.Hour))

			// If sleep duration is negative or very small, sync immediately
			if sleepDuration <= 0 {
				sleepDuration = 0
			}
		}

		log.Printf("Next sync in %v", sleepDuration)
		time.Sleep(sleepDuration)

		// Perform sync
		if err := sync.Sync(); err != nil {
			log.Printf("Sync failed: %v", err)
		}
	}
}
