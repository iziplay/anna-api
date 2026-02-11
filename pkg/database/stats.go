package database

import (
	"errors"
	"sync"
	"time"

	"gorm.io/gorm"
)

// TypeCount represents a count by type
type TypeCount struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
}

// CachedStats holds the cached database statistics
type CachedStats struct {
	LastSync        string      `json:"lastSync"`
	Base            string      `json:"base"`
	Count           int         `json:"count"`
	Identifiers     []TypeCount `json:"identifiers"`
	Classifications []TypeCount `json:"classifications"`
}

// statsCache holds the singleton instance
type statsCache struct {
	mu    sync.RWMutex
	stats *CachedStats
}

var cache = &statsCache{}

// GetCachedStats returns the cached stats if available, nil otherwise
func GetCachedStats() *CachedStats {
	if !cache.mu.TryRLock() {
		return nil
	}
	defer cache.mu.RUnlock()

	return cache.stats
}

// ComputeAndCacheStats computes the stats from the database and stores them in cache
func ComputeAndCacheStats(force bool) *CachedStats {
	if force {
		cache.mu.Lock()
	} else {
		if !cache.mu.TryLock() {
			// Another computation is in progress, return nil to indicate stats are not available
			return nil
		}
	}
	defer cache.mu.Unlock()

	stats := &CachedStats{}

	// Get last full sync date
	var lastSync Synchronization
	err := DB.Where("complete = ?", true).Order("date DESC").First(&lastSync).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// never synchronized, cannot compute stats
		return nil
	}
	if err == nil {
		stats.LastSync = lastSync.Date.Format(time.RFC3339)
		stats.Base = lastSync.Base
	}

	// Count records
	var recordCount int64
	DB.Model(&Record{}).Count(&recordCount)
	stats.Count = int(recordCount)

	// Count identifiers by type
	DB.Model(&RecordIdentifier{}).
		Select("type, COUNT(*) as count").
		Group("type").
		Scan(&stats.Identifiers)

	// Count classifications by type
	DB.Model(&RecordClassification{}).
		Select("type, COUNT(*) as count").
		Group("type").
		Scan(&stats.Classifications)

	cache.stats = stats
	return cache.stats
}

// InvalidateStatsCache marks the cache as invalid so it will be recomputed on next access
func InvalidateStatsCache() {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.stats = nil
}

// HasCachedStats returns whether stats are currently cached
func HasCachedStats() bool {
	cache.mu.RLock()
	defer cache.mu.RUnlock()
	return cache.stats != nil
}
