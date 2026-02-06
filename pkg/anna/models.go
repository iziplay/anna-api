package anna

// Record represents a book/document record from Anna's Archive
type Record struct {
	Index  string       `json:"_index"`
	ID     string       `json:"_id"`
	Score  float64      `json:"_score"`
	Source RecordSource `json:"_source"`
}

// RecordSource contains the main data of the record
type RecordSource struct {
	ID               string           `json:"id"`
	FileUnifiedData  FileUnifiedData  `json:"file_unified_data"`
	SearchOnlyFields SearchOnlyFields `json:"search_only_fields"`
}

// FileUnifiedData contains unified file metadata
type FileUnifiedData struct {
	CoverURLBest           string              `json:"cover_url_best"`
	ExtensionBest          string              `json:"extension_best"`
	FilesizeBest           int64               `json:"filesize_best"`
	TitleBest              string              `json:"title_best"`
	AuthorBest             string              `json:"author_best"`
	PublisherBest          string              `json:"publisher_best"`
	YearBest               string              `json:"year_best"`
	LanguageCodes          []string            `json:"language_codes"`
	ContentTypeBest        string              `json:"content_type_best"`
	IdentifiersUnified     map[string][]string `json:"identifiers_unified"`
	ClassificationsUnified map[string][]string `json:"classifications_unified"`
}

// SearchOnlyFields contains search-specific metadata
type SearchOnlyFields struct {
	SearchFilesize    int64    `json:"search_filesize"`
	SearchYear        string   `json:"search_year"`
	SearchExtension   string   `json:"search_extension"`
	SearchContentType string   `json:"search_content_type"`
	SearchISBN13      []string `json:"search_isbn13"`
	SearchTitle       string   `json:"search_title"`
	SearchAuthor      string   `json:"search_author"`
	SearchPublisher   string   `json:"search_publisher"`
	SearchAddedDate   string   `json:"search_added_date"`
}
