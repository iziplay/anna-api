package routing

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/sse"
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

type DownloadInput struct {
	ID string `path:"id" doc:"Record ID (e.g. md5:abc123)" required:"true"`
}

type DownloadOutput struct {
	ContentType        string `header:"Content-Type"`
	ContentDisposition string `header:"Content-Disposition"`
	Body               []byte
}

type DownloadStatusOutput struct {
	Body struct {
		Status anna.DownloadStatus `json:"status" enum:"NOT_STARTED,DOWNLOADING,DOWNLOADED" doc:"Download status"`
	}
}

// SSE event types for download progress streaming
type DownloadProgressSSE anna.DownloadProgressEvent

type DownloadErrorSSE struct {
	Message string `json:"message"`
}

type SearchByISBNInput struct {
	ISBN      string   `query:"isbn" required:"true" doc:"ISBN10 or ISBN13 code to search for"`
	Languages []string `query:"languages" doc:"Filter by language (strict equality)"`
	Limit     int      `query:"limit" default:"20" minimum:"1" maximum:"100" doc:"Maximum number of results"`
	Offset    int      `query:"offset" default:"0" minimum:"0" doc:"Offset for pagination"`
}

type SearchByTextInput struct {
	Title     string   `query:"title" required:"true" doc:"Filter by title (case-insensitive)"`
	Author    string   `query:"author" required:"true" doc:"Filter by author (case-insensitive)"`
	Publisher string   `query:"publisher" doc:"Filter by publisher (case-insensitive)"`
	Languages []string `query:"languages" doc:"Filter by language (strict equality)"`
	Limit     int      `query:"limit" default:"20" minimum:"1" maximum:"100" doc:"Maximum number of results"`
	Offset    int      `query:"offset" default:"0" minimum:"0" doc:"Offset for pagination"`
}

type SearchOutput struct {
	Body struct {
		Total   int64             `json:"total"`
		Results []database.Record `json:"results"`
	}
}

func Setup(api huma.API) {
	if os.Getenv("ANNA_JWT_SECRET") == "" {
		slog.Warn("ANNA_JWT_SECRET not set, authentication will be disabled")
	}

	api.UseMiddleware(authMiddleware(api))

	huma.Register(api, huma.Operation{
		OperationID: "LivenessCheck",
		Method:      "GET",
		Path:        "/healthz",
		Summary:     "Liveness check",
		Description: "Check if the API process is alive",
		Tags:        []string{"Health"},
	}, func(ctx context.Context, input *struct{}) (*PlainOutput, error) {
		return &PlainOutput{
			ContentType: "text/plain",
			Body:        []byte("OK"),
		}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "ReadinessCheck",
		Method:      "GET",
		Path:        "/readyz",
		Summary:     "Readiness check",
		Description: "Check if the API is ready to serve traffic (database migrated and reachable)",
		Tags:        []string{"Health"},
	}, func(ctx context.Context, input *struct{}) (*PlainOutput, error) {
		if !database.Ready() {
			return nil, huma.Error503ServiceUnavailable("not ready")
		}
		if err := database.Ping(); err != nil {
			return nil, huma.Error503ServiceUnavailable("database not reachable")
		}
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
		OperationID: "CheckDownloadStatus",
		Method:      "GET",
		Path:        "/v1/records/{id}/status",
		Summary:     "Check download status",
		Description: "Check the current status of the epub download for a record",
		Tags:        []string{"Download"},
		Security: []map[string][]string{
			{"bearerAuth": {}},
		},
	}, func(ctx context.Context, input *DownloadInput) (*DownloadStatusOutput, error) {
		filename := fmt.Sprintf("%s.epub", strings.ReplaceAll(input.ID, ":", "_"))
		status := anna.GetDownloadStatus(filename)
		resp := &DownloadStatusOutput{}
		resp.Body.Status = status
		return resp, nil
	})

	sse.Register(api, huma.Operation{
		OperationID: "StreamDownloadProgress",
		Method:      http.MethodGet,
		Path:        "/v1/records/{id}/download/events",
		Summary:     "Stream download progress",
		Description: "Stream real-time download progress events via Server-Sent Events",
		Tags:        []string{"Download"},
		Security: []map[string][]string{
			{"bearerAuth": {}},
		},
	}, map[string]any{
		"progress": DownloadProgressSSE{},
		"error":    DownloadErrorSSE{},
	}, func(ctx context.Context, input *DownloadInput, send sse.Sender) {
		filename := fmt.Sprintf("%s.epub", strings.ReplaceAll(input.ID, ":", "_"))

		// If already downloaded, send a completed event immediately
		status := anna.GetDownloadStatus(filename)
		if status == anna.DownloadStatusDownloaded {
			send.Data(DownloadProgressSSE{
				Status:  anna.DownloadStatusDownloaded,
				Percent: 100,
			})
			return
		}

		progressCh, cleanup := anna.SubscribeDownloadProgress(filename)
		if progressCh == nil {
			send.Data(DownloadErrorSSE{Message: "no active download found, start a download first using the prefetch endpoint"})
			return
		}
		defer cleanup()

		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-progressCh:
				if !ok {
					return
				}
				send.Data(event)
				if event.Status == anna.DownloadStatusDownloaded {
					return
				}
			}
		}
	})

	huma.Register(api, huma.Operation{
		OperationID: "PrefetchRecord",
		Method:      "POST",
		Path:        "/v1/records/{id}/prefetch",
		Summary:     "Prefetch epub",
		Description: "Start downloading the epub file in background",
		Tags:        []string{"Download"},
		Security: []map[string][]string{
			{"bearerAuth": {}},
		},
	}, func(ctx context.Context, input *DownloadInput) (*struct{}, error) {
		info, err := database.GetRecordDownloadInfo(ctx, input.ID)
		if err != nil {
			return nil, huma.Error404NotFound("record download info not found", err)
		}

		torrent, err := database.GetTorrentByClassification(ctx, info.TorrentClassification)
		if err != nil {
			return nil, huma.Error404NotFound("torrent not found", err)
		}

		filename := fmt.Sprintf("%s.epub", strings.ReplaceAll(input.ID, ":", "_"))

		bgCtx := context.WithoutCancel(ctx)
		go func() {
			if _, err := anna.DownloadFile(bgCtx, torrent.MagnetLink, info.ServerPath, torrent.DisplayName, filename); err != nil {
				slog.Error("Failed to prefetch file", "id", input.ID, "error", err)
			}
		}()

		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "DownloadRecord",
		Method:      "GET",
		Path:        "/v1/records/{id}/download",
		Summary:     "Download epub",
		Description: "Download the epub file for a record from its source torrent",
		Tags:        []string{"Download"},
		Security: []map[string][]string{
			{"bearerAuth": {}},
		},
	}, func(ctx context.Context, input *DownloadInput) (*DownloadOutput, error) {
		info, err := database.GetRecordDownloadInfo(ctx, input.ID)
		if err != nil {
			return nil, huma.Error404NotFound("record download info not found", err)
		}

		torrent, err := database.GetTorrentByClassification(ctx, info.TorrentClassification)
		if err != nil {
			return nil, huma.Error404NotFound("torrent not found", err)
		}

		filename := fmt.Sprintf("%s.epub", strings.ReplaceAll(input.ID, ":", "_"))
		data, err := anna.DownloadFile(context.WithoutCancel(ctx), torrent.MagnetLink, info.ServerPath, torrent.DisplayName, filename)
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to download file", err)
		}

		return &DownloadOutput{
			ContentType:        "application/epub+zip",
			ContentDisposition: fmt.Sprintf(`attachment; filename="%s.epub"`, input.ID),
			Body:               data,
		}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "SearchByISBN",
		Method:      "GET",
		Path:        "/v1/search/isbn",
		Summary:     "Search by ISBN",
		Description: "Search for records matching an ISBN10 or ISBN13 code",
		Tags:        []string{"Search"},
	}, func(ctx context.Context, input *SearchByISBNInput) (*SearchOutput, error) {
		records, total, err := database.SearchByISBN(ctx, input.ISBN, input.Languages, input.Limit, input.Offset)
		if err != nil {
			if database.IsValidationError(err) {
				return nil, huma.Error400BadRequest(err.Error())
			}
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
		records, total, err := database.SearchByText(ctx, input.Title, input.Author, input.Publisher, input.Languages, input.Limit, input.Offset)
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to search by text", err)
		}
		resp := &SearchOutput{}
		resp.Body.Total = total
		resp.Body.Results = records
		return resp, nil
	})
}
