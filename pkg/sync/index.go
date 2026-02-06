package sync

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/iziplay/anna-api/pkg/anna"
	"github.com/iziplay/anna-api/pkg/database"
	"gorm.io/gorm"
)

// GetLastSync returns the last sync from database
func GetLastSync() (*database.Synchronization, error) {
	var sync *database.Synchronization
	err := database.DB.Order("date DESC").First(&sync).Error
	if err != nil {
		return nil, err
	}

	return sync, nil
}

// ShouldSync checks if a sync should be performed but only on time value
func ShouldSync() bool {
	lastSync, err := GetLastSync()
	if err != nil {
		return true
	}

	return time.Since(lastSync.Date) > 24*time.Hour
}

func Sync() error {
	lastSync, err := GetLastSync()
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
		err = database.DB.Create(&syncRecord).Error
		GetStatsInstance().EndSync()
		return nil
	}

	log.Printf("Starting sync with torrent: %s", t.MagnetLink)

	// Download and process records in parallel - reading gz while torrent is downloading
	results, err := anna.DownloadAndProcessRecords(t, &annaProcessor{})

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

	//anna.CleanupFiles()

	syncRecord := database.Synchronization{
		Date:     time.Now(),
		Base:     t.DisplayName,
		Complete: true,
	}
	err = database.DB.Create(&syncRecord).Error
	GetStatsInstance().EndSync()
	return err
}

type annaProcessor struct {
	anna.Processor
}

func (*annaProcessor) Files(paths []string) {
	GetStatsInstance().StartSync(paths)
}

func (*annaProcessor) Stats(filePath string, statsType anna.StatsType, percent float64) {
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

func (*annaProcessor) Record(record *anna.Record) {
	database.UpsertRecordAndIdentifiers(record)
}
