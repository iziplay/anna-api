package database

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/iziplay/anna-api/pkg/anna"
	"github.com/lib/pq"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

// DB is the GORM database instance
var DB *gorm.DB

func init() {
	var err error

	DB, err = gorm.Open(postgres.Open(fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		os.Getenv("POSTGRES_HOST"),
		os.Getenv("POSTGRES_USER"),
		os.Getenv("POSTGRES_PASSWORD"),
		os.Getenv("POSTGRES_DATABASE"),
		os.Getenv("POSTGRES_PORT"),
	)), &gorm.Config{
		Logger: logger.New(
			log.Default(),
			logger.Config{
				SlowThreshold:             10 * time.Second,
				LogLevel:                  logger.Warn,
				IgnoreRecordNotFoundError: true,
				Colorful:                  false,
			},
		),
		NamingStrategy: schema.NamingStrategy{
			TablePrefix: "anna_",
		},
	})

	if err != nil {
		slog.Error("Failed to connect to database", "error", err)
		os.Exit(1)
	}

	// Configure connection pool
	sqlDB, err := DB.DB()
	if err != nil {
		slog.Error("Failed to get underlying DB", "error", err)
		os.Exit(1)
	}
	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)

	slog.Info("Database connection established")

	// Auto migrate the schema
	if err := AutoMigrate(); err != nil {
		slog.Error("Failed to auto migrate", "error", err)
		os.Exit(1)
	}
}

// AutoMigrate runs automatic migration for all models
func AutoMigrate() error {
	slog.Info("Running auto migration...")

	// Enable pg_trgm extension for trigram-based ILIKE indexes
	if err := DB.Exec("CREATE EXTENSION IF NOT EXISTS pg_trgm").Error; err != nil {
		return fmt.Errorf("failed to create pg_trgm extension: %w", err)
	}

	// Enable unaccent extension for diacritics-insensitive search
	if err := DB.Exec("CREATE EXTENSION IF NOT EXISTS unaccent").Error; err != nil {
		return fmt.Errorf("failed to create unaccent extension: %w", err)
	}

	// Create a custom text search configuration that strips diacritics.
	DB.Exec("DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_ts_config WHERE cfgname = 'simple_unaccent') THEN CREATE TEXT SEARCH CONFIGURATION simple_unaccent (COPY = simple); ALTER TEXT SEARCH CONFIGURATION simple_unaccent ALTER MAPPING FOR word, numword, asciiword, numhword, asciihword, hword, hword_numpart, hword_part, hword_asciipart WITH unaccent, simple; END IF; END $$")

	err := DB.AutoMigrate(
		&Record{},
		&RecordIdentifier{},
		&RecordClassification{},
		&Synchronization{},
		&Torrent{},
	)

	if err != nil {
		return fmt.Errorf("auto migration failed: %w", err)
	}

	// Create functional GIN indexes for full-text search on text columns.
	// These use to_tsvector('simple_unaccent', ...) to match the @@ expressions
	// in SearchByText and handle diacritics transparently.
	ftsIndexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_record_title_fts ON anna_records USING gin (to_tsvector('simple_unaccent', coalesce(title, '')))",
		"CREATE INDEX IF NOT EXISTS idx_record_author_fts ON anna_records USING gin (to_tsvector('simple_unaccent', coalesce(author, '')))",
		"CREATE INDEX IF NOT EXISTS idx_record_publisher_fts ON anna_records USING gin (to_tsvector('simple_unaccent', coalesce(publisher, '')))",
	}
	for _, ddl := range ftsIndexes {
		if err := DB.Exec(ddl).Error; err != nil {
			return fmt.Errorf("failed to create FTS index: %w", err)
		}
	}

	slog.Info("Auto migration completed successfully")
	return nil
}

// Ping checks the database connection
func Ping() error {
	sqlDB, err := DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Ping()
}

// UpsertTorrents upserts a list of torrents into the database, keyed by BTIH.
func UpsertTorrents(ctx context.Context, torrents []anna.TorrentsResponse) error {
	if len(torrents) == 0 {
		return nil
	}

	records := make([]Torrent, len(torrents))
	for i, t := range torrents {
		records[i] = Torrent{
			BTIH:                  t.BTIH,
			DisplayName:           t.DisplayName,
			URL:                   t.URL,
			MagnetLink:            t.MagnetLink,
			TopLevelGroupName:     t.TopLevelGroupName,
			GroupName:             t.GroupName,
			Obsolete:              t.Obsolete,
			AddedToTorrentsListAt: t.AddedToTorrentsListAt,
		}
	}

	if err := DB.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "btih"}},
		DoUpdates: clause.AssignmentColumns([]string{"display_name", "url", "magnet_link", "top_level_group_name", "group_name", "obsolete", "added_to_torrents_list_at", "updated_at"}),
	}).CreateInBatches(&records, 1000).Error; err != nil {
		return fmt.Errorf("failed to upsert torrents: %w", err)
	}

	return nil
}

// sanitizeString removes null bytes which PostgreSQL rejects in text fields
func sanitizeString(s string) string {
	return strings.ReplaceAll(s, "\x00", "")
}

// UpsertRecordAndIdentifiers creates or updates a record and its identifiers from an Anna record
func UpsertRecordAndIdentifiers(ctx context.Context, annaRecord *anna.Record) error {
	if annaRecord == nil {
		return nil
	}

	if annaRecord.Source.FileUnifiedData.ExtensionBest != "epub" {
		return nil
	}

	year, _ := strconv.Atoi(annaRecord.Source.FileUnifiedData.YearBest)

	// Sanitize language codes
	languages := make([]string, len(annaRecord.Source.FileUnifiedData.LanguageCodes))
	for i, lang := range annaRecord.Source.FileUnifiedData.LanguageCodes {
		languages[i] = sanitizeString(lang)
	}

	record := Record{
		ID:        sanitizeString(annaRecord.ID),
		Title:     sanitizeString(annaRecord.Source.FileUnifiedData.TitleBest),
		Publisher: sanitizeString(annaRecord.Source.FileUnifiedData.PublisherBest),
		Author:    sanitizeString(annaRecord.Source.FileUnifiedData.AuthorBest),
		CoverURL:  sanitizeString(annaRecord.Source.FileUnifiedData.CoverURLBest),
		Year:      year,
		Languages: pq.StringArray(languages),
	}

	if annaRecord.Source.FileUnifiedData.StrippedDescriptionBest != "" {
		record.Description = sanitizeString(annaRecord.Source.FileUnifiedData.StrippedDescriptionBest)
	}

	// Upsert the record using ON CONFLICT
	if err := DB.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"title", "publisher", "author", "cover_url", "year", "languages", "description", "updated_at"}),
	}).Create(&record).Error; err != nil {
		return fmt.Errorf("failed to upsert record: %w", err)
	}

	// Batch upsert identifiers
	var identifiers []RecordIdentifier
	for identifierType, values := range annaRecord.Source.FileUnifiedData.IdentifiersUnified {
		for _, value := range values {
			identifiers = append(identifiers, RecordIdentifier{
				Record: record.ID,
				Type:   sanitizeString(identifierType),
				Value:  sanitizeString(value),
			})
		}
	}

	if len(identifiers) > 0 {
		if err := DB.WithContext(ctx).Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "record"}, {Name: "type"}, {Name: "value"}},
			DoUpdates: clause.AssignmentColumns([]string{"updated_at"}),
		}).Create(&identifiers).Error; err != nil {
			return fmt.Errorf("failed to upsert identifiers: %w", err)
		}
	}

	// Batch upsert classifications
	var classifications []RecordClassification
	for classificationType, values := range annaRecord.Source.FileUnifiedData.ClassificationsUnified {
		for _, value := range values {
			classifications = append(classifications, RecordClassification{
				Record: record.ID,
				Type:   sanitizeString(classificationType),
				Value:  sanitizeString(value),
			})
		}
	}

	if len(classifications) > 0 {
		if err := DB.WithContext(ctx).Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "record"}, {Name: "type"}, {Name: "value"}},
			DoUpdates: clause.AssignmentColumns([]string{"updated_at"}),
		}).Create(&classifications).Error; err != nil {
			return fmt.Errorf("failed to upsert classifications: %w", err)
		}
	}

	return nil
}
