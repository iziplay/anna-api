package sync

import (
	"sync"

	"github.com/iziplay/anna-api/pkg/database"
)

// FileProgress tracks progress for a single file
type FileProgress struct {
	Name       string  `json:"name"`
	Downloaded float64 `json:"downloaded"` // percentage 0-100
	Processed  float64 `json:"processed"`  // percentage 0-100
}

// SyncStats holds the current sync progress information
type SyncStats struct {
	mu        sync.RWMutex
	IsRunning bool           `json:"isRunning"`
	Base      string         `json:"base"`
	Files     []FileProgress `json:"files"`
}

var stats *SyncStats = &SyncStats{}

// GetStats returns a copy of current sync stats
func GetStats() SyncStats {
	stats.mu.RLock()
	defer stats.mu.RUnlock()

	return *stats
}

// GetStatsInstance returns the stats instance for updating
func GetStatsInstance() *SyncStats {
	return stats
}

// StartSync initializes sync statistics
func (s *SyncStats) StartSync(base string, files []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.IsRunning = true
	s.Base = base
	s.Files = make([]FileProgress, len(files))
	for i, name := range files {
		s.Files[i] = FileProgress{
			Name:       name,
			Downloaded: 0,
			Processed:  0,
		}
	}
}

// UpdateFileDownload updates download progress for a file
func (s *SyncStats) UpdateFileDownload(index int, percent float64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if index >= 0 && index < len(s.Files) {
		s.Files[index].Downloaded = percent
	}
}

// UpdateFileProcessed updates processing progress for a file
func (s *SyncStats) UpdateFileProcessed(index int, percent float64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if index >= 0 && index < len(s.Files) {
		s.Files[index].Processed = percent
	}
}

// EndSync marks the sync as completed and refreshes the stats cache
func (s *SyncStats) EndSync() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.IsRunning = false
	s.Base = ""
	s.Files = nil

	database.ComputeAndCacheStats(true)
}
