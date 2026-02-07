package database

import "strings"

// SearchByISBN finds records matching an ISBN10 or ISBN13 value.
// It looks up the record_identifiers table for types "isbn10" and "isbn13".
func SearchByISBN(isbn string, limit, offset int) ([]Record, int64, error) {
	isbn = strings.TrimSpace(isbn)

	var identifiers []RecordIdentifier
	if err := DB.
		Where("type IN ? AND value = ?", []string{"isbn10", "isbn13"}, isbn).
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
	DB.Model(&Record{}).Where("id IN ?", recordIDs).Count(&total)

	var records []Record
	if err := DB.
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
