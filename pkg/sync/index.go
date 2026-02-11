package sync

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/iziplay/anna-api/pkg/anna"
	"github.com/iziplay/anna-api/pkg/database"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

var tracer = otel.Tracer("github.com/iziplay/anna-api/pkg/sync")

// syncBase holds the current torrent display name for stats reporting
var syncBase string

// GetLastSync returns the last sync from database
func GetLastSync(ctx context.Context) (*database.Synchronization, error) {
	ctx, span := tracer.Start(ctx, "GetLastSync")
	defer span.End()

	var sync *database.Synchronization
	err := database.DB.WithContext(ctx).Order("date DESC").First(&sync).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	return sync, nil
}

func Sync(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "Sync")
	defer span.End()

	lastSync, err := GetLastSync(ctx)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("cannot sync: %w", err)
	}

	at, err := anna.FetchTorrentsList()
	if err != nil {
		return err
	}
	log.Printf("Fetched %d torrents from Anna repository", len(at))

	t := anna.GetLastMetadataTorrent(at)
	if t == nil {
		return fmt.Errorf("no metadata torrent found")
	}

	if lastSync != nil && lastSync.Base == t.DisplayName {
		log.Printf("Sync already performed with this torrent: %s", t.DisplayName)
		syncRecord := database.Synchronization{
			Date: time.Now(),
			Base: t.DisplayName,
		}
		err = database.DB.WithContext(ctx).Create(&syncRecord).Error
		GetStatsInstance().EndSync()
		return nil
	}

	log.Printf("Starting sync with torrent: %s", t.MagnetLink)

	// Store the base name for sync stats
	syncBase = t.DisplayName

	// Download and process records in parallel - reading gz while torrent is downloading
	results, err := anna.DownloadAndProcessRecords(ctx, t, &annaProcessor{})

	if err != nil {
		GetStatsInstance().EndSync()
		return err
	}

	// Check for any file processing errors and count total records
	totalRecords := 0
	for _, result := range results {
		if result.Error != nil {
			GetStatsInstance().EndSync()
			return fmt.Errorf("error processing file %s: %w", result.FilePath, result.Error)
		}
		totalRecords += result.RecordCount
	}

	log.Printf("Sync completed successfully: processed %d records from %d files", totalRecords, len(results))

	if os.Getenv("ANNA_KEEP_FILES") != "true" {
		anna.CleanupFiles()
	}

	syncRecord := database.Synchronization{
		Date:     time.Now(),
		Base:     t.DisplayName,
		Complete: true,
	}
	err = database.DB.WithContext(ctx).Create(&syncRecord).Error
	GetStatsInstance().EndSync()
	return err
}

type annaProcessor struct {
	anna.Processor
}

func (*annaProcessor) Files(ctx context.Context, paths []string) {
	GetStatsInstance().StartSync(syncBase, paths)
}

func (*annaProcessor) Stats(ctx context.Context, filePath string, statsType anna.StatsType, percent float64) {
	statsInstance := GetStatsInstance()
	var fileIndex int = -1

	statsInstance.mu.RLock()
	for i, file := range statsInstance.Files {
		if file.Name == filePath {
			fileIndex = i
			break
		}
	}
	statsInstance.mu.RUnlock()

	if fileIndex == -1 {
		return
	}

	switch statsType {
	case anna.StatsTypeFileDownload:
		statsInstance.UpdateFileDownload(fileIndex, percent)
	case anna.StatsTypeFileProcessing:
		statsInstance.UpdateFileProcessed(fileIndex, percent)
	}
}

func (*annaProcessor) Record(ctx context.Context, record *anna.Record) {
	database.UpsertRecordAndIdentifiers(ctx, record)
}
