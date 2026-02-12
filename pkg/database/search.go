package database

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/iziplay/anna-api/pkg/isbn"
	"github.com/lib/pq"
)

var errValidation = errors.New("validation error")

// IsValidationError checks whether an error is a validation error.
func IsValidationError(err error) bool {
	return errors.Is(err, errValidation)
}

// SearchByISBN finds records matching an ISBN10 or ISBN13 value.
// It also computes the alternate ISBN form and searches for both.
func SearchByISBN(ctx context.Context, isbnCode string, languages []string, limit, offset int) ([]Record, int64, error) {
	isbnCode = strings.TrimSpace(isbnCode)

	if len(isbnCode) != 10 && len(isbnCode) != 13 {
		return nil, 0, fmt.Errorf("invalid ISBN length: expected 10 or 13 characters, got %d: %w", len(isbnCode), errValidation)
	}

	// Build the set of ISBNs to search for
	isbns := []string{isbnCode}
	if len(isbnCode) == 10 {
		if isbn13 := isbn.To13(isbnCode); isbn13 != "" {
			isbns = append(isbns, isbn13)
		}
	} else if len(isbnCode) == 13 && strings.HasPrefix(isbnCode, "978") {
		if isbn10 := isbn.To10(isbnCode); isbn10 != "" {
			isbns = append(isbns, isbn10)
		}
	}

	slog.DebugContext(ctx, "Searching by ISBN", "input", isbnCode, "search_isbns", isbns, "languages", languages, "limit", limit, "offset", offset)

	var identifiers []RecordIdentifier
	if err := DB.
		WithContext(ctx).
		Where("type IN ? AND value IN ?", []string{"isbn10", "isbn13"}, isbns).
		Find(&identifiers).Error; err != nil {
		return nil, 0, err
	}

	if len(identifiers) == 0 {
		return []Record{}, 0, nil
	}

	// Collect unique record IDs
	idSet := make(map[string]struct{})
	for _, id := range identifiers {
		idSet[id.Record] = struct{}{}
	}
	recordIDs := make([]string, 0, len(idSet))
	for id := range idSet {
		recordIDs = append(recordIDs, id)
	}

	q := DB.Model(&Record{}).WithContext(ctx).Where("id IN ?", recordIDs)

	if len(languages) > 0 {
		q = q.Where("languages = ?", pq.StringArray(languages))
	}

	var total int64
	q.Count(&total)

	var records []Record
	if err := q.
		Preload("Identifiers").
		Preload("Classifications").
		Limit(limit).
		Offset(offset).
		Find(&records).Error; err != nil {
		return nil, 0, err
	}

	return records, total, nil
}

// SearchByText finds records matching the given title, author, and/or publisher filters (AND logic, case-insensitive).
func SearchByText(title, author, publisher string, languages []string, limit, offset int) ([]Record, int64, error) {
	q := DB.Model(&Record{})

	if t := strings.TrimSpace(title); t != "" {
		q = q.Where("title ILIKE ?", "%"+t+"%")
	}
	if a := strings.TrimSpace(author); a != "" {
		q = q.Where("author ILIKE ?", "%"+a+"%")
	}
	if p := strings.TrimSpace(publisher); p != "" {
		q = q.Where("publisher ILIKE ?", "%"+p+"%")
	}
	if len(languages) > 0 {
		q = q.Where("languages = ?", pq.StringArray(languages))
	}

	var total int64

	q.Count(&total)

	var records []Record
	if err := q.
		Preload("Identifiers").
		Preload("Classifications").
		Limit(limit).
		Offset(offset).
		Find(&records).Error; err != nil {
		return nil, 0, err
	}

	return records, total, nil
}

// GetRecordByID returns a single record by its ID, or nil if not found.
func GetRecordByID(id string) (*Record, error) {
	var record Record
	if err := DB.
		Preload("Identifiers").
		Preload("Classifications").
		Where("id = ?", id).
		First(&record).Error; err != nil {
		return nil, err
	}
	return &record, nil
}

// RecordDownloadInfo contains the information needed to download a record's file.
type RecordDownloadInfo struct {
	TorrentClassification string // e.g., "managed_by_aa/zlib/pilimi-zlib-6160000-7229999.torrent"
	ServerPath            string // e.g., "g5/zlib1/zlib1/pilimi-zlib-6160000-7229999/7225029"
}

// GetRecordDownloadInfo retrieves the torrent classification and server_path for downloading a record's file.
func GetRecordDownloadInfo(id string) (*RecordDownloadInfo, error) {
	var torrentClasses []RecordClassification
	if err := DB.Where("record = ? AND type = ?", id, "torrent").Find(&torrentClasses).Error; err != nil {
		return nil, fmt.Errorf("torrent classifications lookup failed: %w", err)
	}
	if len(torrentClasses) == 0 {
		return nil, fmt.Errorf("no torrent classifications found")
	}

	var serverPathIdents []RecordIdentifier
	if err := DB.Where("record = ? AND type = ?", id, "server_path").Find(&serverPathIdents).Error; err != nil {
		return nil, fmt.Errorf("server_path identifiers lookup failed: %w", err)
	}
	if len(serverPathIdents) == 0 {
		return nil, fmt.Errorf("no server_path identifiers found")
	}

	// Try to find a matching pair where the server path contains the torrent filename (without extension)
	for _, tc := range torrentClasses {
		// tc.Value example: "managed_by_aa/zlib/pilimi-zlib-6160000-7229999.torrent"

		// Extract base name from torrent path
		parts := strings.Split(tc.Value, "/")
		fileName := parts[len(parts)-1]
		baseName := strings.TrimSuffix(fileName, ".torrent")

		for _, sp := range serverPathIdents {
			// sp.Value example: "g5/zlib1/zlib1/pilimi-zlib-6160000-7229999/7225029"

			if strings.Contains(sp.Value, baseName) {
				return &RecordDownloadInfo{
					TorrentClassification: tc.Value,
					ServerPath:            sp.Value,
				}, nil
			}
		}
	}

	// Fallback to the first available pair if no clear match is found
	return &RecordDownloadInfo{
		TorrentClassification: torrentClasses[0].Value,
		ServerPath:            serverPathIdents[0].Value,
	}, nil
}

// GetTorrentByClassification finds a torrent whose URL ends with the given classification value.
func GetTorrentByClassification(classification string) (*Torrent, error) {
	var t Torrent
	if err := DB.Where("url LIKE ?", "%"+classification).First(&t).Error; err != nil {
		return nil, fmt.Errorf("torrent not found for classification %s: %w", classification, err)
	}
	return &t, nil
}
