package database

import (
	"time"

	"github.com/lib/pq"
)

type Model struct {
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Record struct {
	Model

	ID        string         `json:"id" gorm:"primaryKey"`
	Title     string         `json:"title"`
	Publisher string         `json:"publisher"`
	Author    string         `json:"author"`
	CoverURL  string         `json:"coverURL"`
	Year      int            `json:"year"`
	Languages pq.StringArray `json:"languages" gorm:"type:text[]"`

	Identifiers     []RecordIdentifier     `json:"identifiers" gorm:"foreignKey:Record;references:ID"`
	Classifications []RecordClassification `json:"classifications" gorm:"foreignKey:Record;references:ID"`
}

type RecordIdentifier struct {
	Model

	Record string `json:"record" gorm:"primaryKey"`
	Type   string `json:"type" gorm:"primaryKey;index:idx_record_identifier_type;index:idx_record_identifier_type_value"`
	Value  string `json:"value" gorm:"primaryKey;index:idx_record_identifier_type_value"`
}

type RecordClassification struct {
	Model

	Record string `json:"record" gorm:"primaryKey"`
	Type   string `json:"type" gorm:"primaryKey;index:idx_record_classification_type;index:idx_record_classification_type_value"`
	Value  string `json:"value" gorm:"primaryKey;index:idx_record_classification_type_value"`
}

type Synchronization struct {
	Date     time.Time `gorm:"primaryKey;type:timestamptz"`
	Base     string    // the database used for this sync, e.g.: "aa_derived_mirror_metadata_20240612.torrent"
	Complete bool
}
