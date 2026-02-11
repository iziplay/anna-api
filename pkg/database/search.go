package database

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/iziplay/anna-api/pkg/isbn"
)

var errValidation = errors.New("validation error")

// IsValidationError checks whether an error is a validation error.
func IsValidationError(err error) bool {
	return errors.Is(err, errValidation)
}

// SearchByISBN finds records matching an ISBN10 or ISBN13 value.
// It also computes the alternate ISBN form and searches for both.
func SearchByISBN(ctx context.Context, isbnCode string, limit, offset int) ([]Record, int64, error) {
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

	slog.DebugContext(ctx, "Searching by ISBN", "input", isbnCode, "search_isbns", isbns, "limit", limit, "offset", offset)

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

	var total int64
	DB.Model(&Record{}).WithContext(ctx).Where("id IN ?", recordIDs).Count(&total)

	var records []Record
	if err := DB.
		WithContext(ctx).
		Preload("Identifiers").
		Preload("Classifications").
		Where("id IN ?", recordIDs).
		Limit(limit).
		Offset(offset).
		Find(&records).Error; err != nil {
		return nil, 0, err
	}

	return records, total, nil
}

// SearchByText finds records matching the given title, author, and/or publisher filters (AND logic, case-insensitive).
func SearchByText(title, author, publisher string, limit, offset int) ([]Record, int64, error) {
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
