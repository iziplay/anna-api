package anna

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
)

var DataDir = "/tmp/anna-torrents"

// digitPattern matches the digit in filenames like "aarecords__7.json.gz"
var digitPattern = regexp.MustCompile(`aarecords__(\d+)\.json\.gz$`)

// ExtractFileIndex extracts the numeric index from a filename like "aarecords__7.json.gz"
func ExtractFileIndex(path string) (int, error) {
	matches := digitPattern.FindStringSubmatch(path)
	if len(matches) < 2 {
		return 0, fmt.Errorf("no digit found in path: %s", path)
	}
	var index int
	_, err := fmt.Sscanf(matches[1], "%d", &index)
	if err != nil {
		return 0, fmt.Errorf("failed to parse digit: %w", err)
	}
	return index, nil
}

func init() {
	if dir, ok := os.LookupEnv("ANNA_TORRENT_DATA_DIR"); ok {
		slog.Info("Using custom Anna torrent data dir", "dir", dir)
		DataDir = dir
	}
}

// RecordProcessor is a callback function that processes a single record
type RecordProcessor func(record *Record) error

// FileResult represents the result of processing a single file
type FileResult struct {
	FilePath    string
	RecordCount int
	Error       error
}

var client *torrent.Client

func init() {
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = DataDir
	cfg.Seed = true // we drop torrents manually after processing
	var err error
	client, err = torrent.NewClient(cfg)
	if err != nil {
		panic(err)
	}
}

func GetTorrentStats() string {
	status := &bytes.Buffer{}
	client.WriteStatus(status)
	return status.String()
}

type StatsType string

const (
	StatsTypeFileDownload   StatsType = "download"
	StatsTypeFileProcessing StatsType = "processing"
)

type Processor interface {
	Files(ctx context.Context, paths []string)
	Stats(ctx context.Context, path string, key StatsType, value float64)
	Record(ctx context.Context, record *Record)
}

// DownloadAndProcessRecords downloads torrent files and processes records in parallel as they download
func DownloadAndProcessRecords(ctx context.Context, torrentResponse *TorrentsResponse, processor Processor) ([]FileResult, error) {
	t, err := client.AddMagnet(torrentResponse.MagnetLink)
	if err != nil {
		return nil, fmt.Errorf("failed to add magnet: %w", err)
	}

	slog.Info("Waiting for torrent info...")
	<-t.GotInfo()

	filePattern := regexp.MustCompile(`elasticsearch/aarecords__\d+\.json\.gz$`)
	if os.Getenv("ANNA_ARCHIVE_ID") != "" {
		filePattern = regexp.MustCompile(fmt.Sprintf(`elasticsearch/aarecords__%s\.json\.gz$`, os.Getenv("ANNA_ARCHIVE_ID")))
	}

	var matchedFiles []*torrent.File
	for _, file := range t.Files() {
		if filePattern.MatchString(file.Path()) {
			matchedFiles = append(matchedFiles, file)
			slog.Info("Found matching file", "path", file.Path())
		}
	}

	if len(matchedFiles) == 0 {
		slog.Warn("No matching files found in torrent")
		return nil, nil
	}

	// Sort files by index for prioritized downloading
	sort.Slice(matchedFiles, func(i, j int) bool {
		indexI, _ := ExtractFileIndex(path.Base(matchedFiles[i].Path()))
		indexJ, _ := ExtractFileIndex(path.Base(matchedFiles[j].Path()))
		return indexI < indexJ
	})

	// Initialize stats with file names
	fileNames := make([]string, len(matchedFiles))
	for i, file := range matchedFiles {
		fileNames[i] = file.Path()
		file.Download()
	}
	processor.Files(ctx, fileNames)

	slog.Info("Starting download and processing of files in parallel", "count", len(matchedFiles))

	var results []FileResult
	var resultsMu sync.Mutex

	var wg sync.WaitGroup
	for _, file := range matchedFiles {
		wg.Add(1)
		base := path.Base(file.Path())
		index, err := ExtractFileIndex(base)
		if err != nil {
			panic(err)
		}
		go func(index int, f *torrent.File) {
			defer wg.Done()
			result := processFileWhileDownloading(ctx, index, f, processor)
			if result.Error != nil {
				e := fmt.Errorf("cannot process file %d: %w", index, result.Error)
				panic(e)
			}
			resultsMu.Lock()
			results = append(results, result)
			resultsMu.Unlock()
		}(index, file)
	}

	// Start progress monitoring in background
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				updateProgress(ctx, matchedFiles, processor)
			}
		}
	}()

	// Wait for all processing to complete
	wg.Wait()
	close(done)

	// Drop the torrent to free resources - files will remain on disk for any post-processing if needed
	t.Drop()

	slog.Info("All files processed successfully")

	return results, nil
}

// processFileWhileDownloading reads and processes a gz file while it's being downloaded
func processFileWhileDownloading(ctx context.Context, index int, file *torrent.File, processor Processor) FileResult {
	result := FileResult{
		FilePath: file.Path(),
	}

	slog.Info("Starting to process file", "index", index, "path", file.Path())

	// Get a reader from the torrent file - this will block until data is available
	reader := file.NewReader()
	defer reader.Close()

	// Set read-ahead to allow sequential reading with some buffer
	reader.SetReadahead(10 * 1024 * 1024) // 10MB read-ahead

	// Create gzip reader directly from torrent stream
	gzReader, err := gzip.NewReader(reader)
	if err != nil {
		result.Error = fmt.Errorf("failed to create gzip reader: %w", err)
		return result
	}
	defer gzReader.Close()

	// Create buffered reader for line-by-line processing
	bufReader := bufio.NewReaderSize(gzReader, 4*1024*1024) // 4MB buffer

	recordCount := 0
	lineCount := 0

	for {
		line, err := bufReader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			if errors.Is(err, io.ErrUnexpectedEOF) {
				slog.Warn("Unexpected EOF reached, ending processing", "line", lineCount+1)
				break
			}
			result.Error = fmt.Errorf("read error at line %d: %w", lineCount+1, err)
			return result
		}

		lineCount++

		var record Record
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			slog.Warn("Failed to parse JSON, skipping", "line", lineCount, "file", file.Path(), "error", err)
			continue
		}

		processor.Record(ctx, &record)

		recordCount++

		// Update progress every 10000 records
		if recordCount%10000 == 0 {
			// Calculate approximate progress based on bytes read from compressed stream
			bytesCompleted := file.BytesCompleted()
			totalBytes := file.Length()
			progress := 0.0
			if totalBytes > 0 {
				progress = float64(bytesCompleted) / float64(totalBytes) * 100
			}
			processor.Stats(ctx, file.Path(), StatsTypeFileProcessing, progress)
		}
	}

	processor.Stats(ctx, file.Path(), StatsTypeFileProcessing, 100.0)
	processor.Stats(ctx, file.Path(), StatsTypeFileDownload, 100.0)

	result.RecordCount = recordCount
	return result
}

// processFileFromDisk reads and processes a gz file after it has been fully downloaded to disk
// This is more efficient than streaming as it avoids torrent protocol overhead during reads
func processFileFromDisk(ctx context.Context, index int, file *torrent.File, processor Processor) FileResult {
	result := FileResult{
		FilePath: file.Path(),
	}

	slog.Info("Waiting for file to complete download", "index", index, "path", file.Path())

	// Wait for the file to be fully downloaded
	for file.BytesCompleted() < file.Length() {
		time.Sleep(500 * time.Millisecond)
	}

	slog.Info("File download complete, processing from disk", "index", index, "path", file.Path())

	// Construct the full path to the file on disk
	filePath := path.Join(DataDir, file.Path())

	// Step 1: Open and decompress the gzip file
	diskFile, err := os.Open(filePath)
	if err != nil {
		result.Error = fmt.Errorf("failed to open file from disk: %w", err)
		return result
	}
	defer diskFile.Close()

	slog.Info("Decompressing gzip to temp file", "index", index)

	gzReader, err := gzip.NewReader(diskFile)
	if err != nil {
		result.Error = fmt.Errorf("failed to create gzip reader: %w", err)
		return result
	}

	// Create a temporary file for decompressed data
	tempFile, err := os.CreateTemp("", fmt.Sprintf("anna-decompressed-%d-*.json", index))
	if err != nil {
		gzReader.Close()
		result.Error = fmt.Errorf("failed to create temp file: %w", err)
		return result
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath) // Clean up temp file when done

	// Decompress to temp file
	written, err := io.Copy(tempFile, gzReader)
	gzReader.Close()
	tempFile.Close()
	diskFile.Close()
	if err != nil {
		result.Error = fmt.Errorf("failed to decompress gzip: %w", err)
		return result
	}

	slog.Info("Decompressed to temp file, now parsing JSON", "index", index, "bytes", written)

	// Step 2: Read the JSON from the decompressed temp file
	jsonFile, err := os.Open(tempPath)
	if err != nil {
		result.Error = fmt.Errorf("failed to open temp file: %w", err)
		return result
	}
	defer jsonFile.Close()

	bufReader := bufio.NewReaderSize(jsonFile, 8*1024*1024) // 8MB buffer

	recordCount := 0
	lineCount := 0

	for {
		line, err := bufReader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			result.Error = fmt.Errorf("read error at line %d: %w", lineCount+1, err)
			return result
		}

		lineCount++

		var record Record
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			slog.Warn("Failed to parse JSON, skipping", "line", lineCount, "file", file.Path(), "error", err)
			continue
		}

		processor.Record(ctx, &record)

		recordCount++

		slog.Debug("File processing progress", "index", index, "records", recordCount)
	}

	processor.Stats(ctx, file.Path(), StatsTypeFileProcessing, 100.0)
	processor.Stats(ctx, file.Path(), StatsTypeFileDownload, 100.0)
	slog.Info("Completed file", "index", index, "path", file.Path(), "records", recordCount)

	result.RecordCount = recordCount
	return result
}

func CleanupFiles() error {
	slog.Info("Cleaning up torrent directory", "dir", DataDir)
	return os.RemoveAll(DataDir)
}

func updateProgress(ctx context.Context, files []*torrent.File, processor Processor) {
	for _, file := range files {
		completed := file.BytesCompleted()
		total := file.Length()
		percent := 0.0
		if total > 0 {
			percent = float64(completed) / float64(total) * 100
		}

		processor.Stats(ctx, file.Path(), StatsTypeFileDownload, percent)
	}
}
