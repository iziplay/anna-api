package routing

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/iziplay/anna-api/pkg/anna"
	"github.com/iziplay/anna-api/pkg/database"
	"github.com/iziplay/anna-api/pkg/sync"
)

type BooksOutput struct {
	Body struct {
		Message string `json:"message"`
	}
}

type StatsOutput struct {
	Body database.CachedStats
}

type PlainOutput struct {
	ContentType string `header:"Content-Type"`
	Body        []byte
}

type SyncStatsOutput struct {
	Body sync.SyncStats
}

func Setup(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "HealthCheck",
		Method:      "GET",
		Path:        "/healthz",
		Summary:     "Health check",
		Description: "Check if the API is running",
		Tags:        []string{"Health"},
	}, func(ctx context.Context, input *struct{}) (*PlainOutput, error) {
		return &PlainOutput{
			ContentType: "text/plain",
			Body:        []byte("OK"),
		}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "GetStats",
		Method:      "GET",
		Path:        "/v1/stats",
		Summary:     "Get statistics",
		Description: "Get statistics about current data set",
		Tags:        []string{"Stats"},
	}, func(ctx context.Context, input *struct{}) (*StatsOutput, error) {
		stats := database.GetCachedStats()
		if stats == nil {
			go database.ComputeAndCacheStats(false)
			return nil, huma.Error503ServiceUnavailable("sync in progress or stats are being computed, please retry later")
		}
		return &StatsOutput{
			Body: *stats,
		}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "GetSyncStats",
		Method:      "GET",
		Path:        "/v1/stats/sync",
		Summary:     "Get sync statistics",
		Description: "Get current sync progress and statistics",
		Tags:        []string{"Stats"},
	}, func(ctx context.Context, input *struct{}) (*SyncStatsOutput, error) {
		resp := &SyncStatsOutput{}
		resp.Body = sync.GetStats()
		return resp, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "GetTorrentStats",
		Method:      "GET",
		Path:        "/v1/stats/torrent",
		Summary:     "Get torrent statistics",
		Description: "Get current torrent progress and statistics",
		Tags:        []string{"Stats"},
	}, func(ctx context.Context, input *struct{}) (*PlainOutput, error) {
		resp := &PlainOutput{
			ContentType: "text/plain",
			Body:        []byte(anna.GetTorrentStats()),
		}
		return resp, nil
	})
}
