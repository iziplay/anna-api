package database

import (
	"time"

	"github.com/lib/pq"
)

type Model struct {
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Record struct {
	Model

	ID          string         `json:"id" gorm:"primaryKey"`
	Title       string         `json:"title" gorm:"index:idx_record_title_trgm,type:gin,expression:title gin_trgm_ops"`
	Publisher   string         `json:"publisher" gorm:"index:idx_record_publisher_trgm,type:gin,expression:publisher gin_trgm_ops"`
	Author      string         `json:"author" gorm:"index:idx_record_author_trgm,type:gin,expression:author gin_trgm_ops"`
	CoverURL    string         `json:"coverURL"`
	Year        int            `json:"year"`
	Languages   pq.StringArray `json:"languages" gorm:"type:text[]"`
	Description string         `json:"description,omitempty"`

	Identifiers     []RecordIdentifier     `json:"identifiers" gorm:"foreignKey:Record;references:ID"`
	Classifications []RecordClassification `json:"classifications" gorm:"foreignKey:Record;references:ID"`
}

type RecordIdentifier struct {
	Model

	Record string `json:"-" gorm:"primaryKey"`
	Type   string `json:"type" gorm:"primaryKey;index:idx_record_identifier_type;index:idx_record_identifier_type_value"`
	Value  string `json:"value" gorm:"primaryKey;index:idx_record_identifier_type_value"`
}

type RecordClassification struct {
	Model

	Record string `json:"-" gorm:"primaryKey"`
	Type   string `json:"type" gorm:"primaryKey;index:idx_record_classification_type;index:idx_record_classification_type_value"`
	Value  string `json:"value" gorm:"primaryKey;index:idx_record_classification_type_value"`
}

type Torrent struct {
	Model

	BTIH                  string `json:"btih" gorm:"primaryKey;column:btih"`
	DisplayName           string `json:"display_name"`
	URL                   string `json:"url"`
	MagnetLink            string `json:"magnet_link"`
	TopLevelGroupName     string `json:"top_level_group_name"`
	GroupName             string `json:"group_name"`
	Obsolete              bool   `json:"obsolete"`
	AddedToTorrentsListAt string `json:"added_to_torrents_list_at"`
}

type Synchronization struct {
	Date     time.Time `gorm:"primaryKey;type:timestamptz"`
	Base     string    // the database used for this sync, e.g.: "aa_derived_mirror_metadata_20240612.torrent"
	Complete bool
}
