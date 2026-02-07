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

type SearchByISBNInput struct {
	ISBN   string `query:"isbn" required:"true" doc:"ISBN10 or ISBN13 code to search for"`
	Limit  int    `query:"limit" default:"20" minimum:"1" maximum:"100" doc:"Maximum number of results"`
	Offset int    `query:"offset" default:"0" minimum:"0" doc:"Offset for pagination"`
}

type SearchByTextInput struct {
	Title     string `query:"title" required:"true" doc:"Filter by title (case-insensitive)"`
	Author    string `query:"author" required:"true" doc:"Filter by author (case-insensitive)"`
	Publisher string `query:"publisher" required:"true" doc:"Filter by publisher (case-insensitive)"`
	Limit     int    `query:"limit" default:"20" minimum:"1" maximum:"100" doc:"Maximum number of results"`
	Offset    int    `query:"offset" default:"0" minimum:"0" doc:"Offset for pagination"`
}

type SearchOutput struct {
	Body struct {
		Total   int64             `json:"total"`
		Results []database.Record `json:"results"`
	}
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
		OperationID: "GetStatistics",
		Method:      "GET",
		Path:        "/v1/statistics",
		Summary:     "Get statistics",
		Description: "Get statistics about current data set",
		Tags:        []string{"Statistics"},
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
		OperationID: "GetSyncStatistics",
		Method:      "GET",
		Path:        "/v1/statistics/sync",
		Summary:     "Get sync statistics",
		Description: "Get current sync progress and statistics",
		Tags:        []string{"Statistics"},
	}, func(ctx context.Context, input *struct{}) (*SyncStatsOutput, error) {
		resp := &SyncStatsOutput{}
		resp.Body = sync.GetStats()
		return resp, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "GetTorrentStatistics",
		Method:      "GET",
		Path:        "/v1/statistics/torrent",
		Summary:     "Get torrent statistics",
		Description: "Get current torrent progress and statistics",
		Tags:        []string{"Statistics"},
	}, func(ctx context.Context, input *struct{}) (*PlainOutput, error) {
		resp := &PlainOutput{
			ContentType: "text/plain",
			Body:        []byte(anna.GetTorrentStats()),
		}
		return resp, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "SearchByISBN",
		Method:      "GET",
		Path:        "/v1/search/isbn",
		Summary:     "Search by ISBN",
		Description: "Search for records matching an ISBN10 or ISBN13 code",
		Tags:        []string{"Search"},
	}, func(ctx context.Context, input *SearchByISBNInput) (*SearchOutput, error) {
		records, total, err := database.SearchByISBN(input.ISBN, input.Limit, input.Offset)
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to search by ISBN", err)
		}
		resp := &SearchOutput{}
		resp.Body.Total = total
		resp.Body.Results = records
		return resp, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "SearchByText",
		Method:      "GET",
		Path:        "/v1/search/text",
		Summary:     "Search by text",
		Description: "Search for records by title, author, and publisher",
		Tags:        []string{"Search"},
	}, func(ctx context.Context, input *SearchByTextInput) (*SearchOutput, error) {
		records, total, err := database.SearchByText(input.Title, input.Author, input.Publisher, input.Limit, input.Offset)
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to search by text", err)
		}
		resp := &SearchOutput{}
		resp.Body.Total = total
		resp.Body.Results = records
		return resp, nil
	})
}
