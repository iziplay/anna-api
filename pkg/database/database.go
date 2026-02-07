package database

import (
	"context"
	"fmt"
	"log"
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
		log.Fatalf("Failed to connect to database: %v", err)
	}

	// Configure connection pool
	sqlDB, err := DB.DB()
	if err != nil {
		log.Fatalf("Failed to get underlying DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)

	log.Println("Database connection established")

	// Auto migrate the schema
	if err := AutoMigrate(); err != nil {
		log.Fatalf("Failed to auto migrate: %v", err)
	}
}

// AutoMigrate runs automatic migration for all models
func AutoMigrate() error {
	log.Println("Running auto migration...")

	// Enable pg_trgm extension for trigram-based ILIKE indexes
	if err := DB.Exec("CREATE EXTENSION IF NOT EXISTS pg_trgm").Error; err != nil {
		return fmt.Errorf("failed to create pg_trgm extension: %w", err)
	}

	err := DB.AutoMigrate(
		&Record{},
		&RecordIdentifier{},
		&RecordClassification{},
		&Synchronization{},
	)

	if err != nil {
		return fmt.Errorf("auto migration failed: %w", err)
	}

	log.Println("Auto migration completed successfully")
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

	// Upsert the record using ON CONFLICT
	if err := DB.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"title", "publisher", "author", "cover_url", "year", "languages", "updated_at"}),
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
