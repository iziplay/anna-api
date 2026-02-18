package anna

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"golang.org/x/sync/singleflight"
)

var (
	EpubStorageDir  = "/tmp/anna-epubs"
	g               singleflight.Group
	activeDownloads sync.Map
)

const (
	DownloadStatusNotStarted  DownloadStatus = "NOT_STARTED"
	DownloadStatusDownloading DownloadStatus = "DOWNLOADING"
	DownloadStatusDownloaded  DownloadStatus = "DOWNLOADED"
)

type DownloadStatus string

// DownloadProgressEvent represents a download progress update
type DownloadProgressEvent struct {
	Status         DownloadStatus `json:"status"`
	BytesCompleted int64          `json:"bytes_completed"`
	TotalBytes     int64          `json:"total_bytes"`
	Percent        float64        `json:"percent"`
}

type downloadTracker struct {
	mu          sync.RWMutex
	progress    DownloadProgressEvent
	subscribers []chan DownloadProgressEvent
}

func newDownloadTracker() *downloadTracker {
	return &downloadTracker{
		progress: DownloadProgressEvent{
			Status: DownloadStatusDownloading,
		},
	}
}

func (t *downloadTracker) update(bytesCompleted, totalBytes int64) {
	t.mu.Lock()
	percent := 0.0
	if totalBytes > 0 {
		percent = float64(bytesCompleted) / float64(totalBytes) * 100
	}
	t.progress = DownloadProgressEvent{
		Status:         DownloadStatusDownloading,
		BytesCompleted: bytesCompleted,
		TotalBytes:     totalBytes,
		Percent:        percent,
	}
	subs := make([]chan DownloadProgressEvent, len(t.subscribers))
	copy(subs, t.subscribers)
	progress := t.progress
	t.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- progress:
		default:
		}
	}
}

func (t *downloadTracker) complete() {
	t.mu.Lock()
	t.progress.Status = DownloadStatusDownloaded
	t.progress.Percent = 100
	progress := t.progress
	subs := t.subscribers
	t.subscribers = nil
	t.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- progress:
		default:
		}
		close(ch)
	}
}

func (t *downloadTracker) subscribe() (<-chan DownloadProgressEvent, func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	ch := make(chan DownloadProgressEvent, 10)
	t.subscribers = append(t.subscribers, ch)
	// Send current state immediately
	ch <- t.progress
	cleanup := func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		for i, sub := range t.subscribers {
			if sub == ch {
				t.subscribers = append(t.subscribers[:i], t.subscribers[i+1:]...)
				break
			}
		}
	}
	return ch, cleanup
}

// SubscribeDownloadProgress subscribes to download progress events for a file.
// Returns a channel of progress events and a cleanup function.
// Returns nil if no active download is found.
func SubscribeDownloadProgress(outputFilename string) (<-chan DownloadProgressEvent, func()) {
	val, ok := activeDownloads.Load(outputFilename)
	if !ok {
		return nil, nil
	}
	tracker := val.(*downloadTracker)
	return tracker.subscribe()
}

func GetDownloadStatus(outputFilename string) DownloadStatus {
	if EpubStorageDir != "" {
		path := filepath.Join(EpubStorageDir, outputFilename)
		if _, err := os.Stat(path); err == nil {
			return DownloadStatusDownloaded
		}
	}

	if _, ok := activeDownloads.Load(outputFilename); ok {
		return DownloadStatusDownloading
	}

	return DownloadStatusNotStarted
}

func init() {
	if dir, ok := os.LookupEnv("ANNA_EPUB_STORAGE_DIR"); ok {
		slog.Info("Using custom Anna epub storage dir", "dir", dir)
		EpubStorageDir = dir
	}
}

// DownloadFile downloads a specific file from a torrent and returns its contents.
// magnetLink is the magnet link for the torrent.
// serverPath is the server_path identifier value (e.g., "g5/zlib1/zlib1/pilimi-zlib-6160000-7229999/7225029").
// torrentName is the torrent display name (e.g., "pilimi-zlib-6160000-7229999.torrent").
// outputFilename is the name of the file to be saved in the storage directory.
func DownloadFile(ctx context.Context, magnetLink, serverPath, torrentName, outputFilename string) ([]byte, error) {
	// 1. Check if file exists in storage
	if EpubStorageDir != "" {
		path := filepath.Join(EpubStorageDir, outputFilename)
		if data, err := os.ReadFile(path); err == nil {
			slog.Info("File found in storage", "path", path)
			return data, nil
		}
	}

	// 2. Use singleflight to prevent multiple concurrent downloads for the same file
	newTracker := newDownloadTracker()
	actual, _ := activeDownloads.LoadOrStore(outputFilename, newTracker)
	tracker := actual.(*downloadTracker)

	key := fmt.Sprintf("%s-%s", torrentName, outputFilename)
	v, err, _ := g.Do(key, func() (interface{}, error) {
		defer func() {
			tracker.complete()
			activeDownloads.Delete(outputFilename)
		}()
		// Double-check cache inside singleflight in case another goroutine just finished downloading it
		if EpubStorageDir != "" {
			path := filepath.Join(EpubStorageDir, outputFilename)
			if data, err := os.ReadFile(path); err == nil {
				return data, nil
			}
		}
		return downloadFileInternal(ctx, magnetLink, serverPath, torrentName, outputFilename, tracker)
	})

	if err != nil {
		return nil, err
	}
	return v.([]byte), nil
}

func downloadFileInternal(ctx context.Context, magnetLink, serverPath, torrentName, outputFilename string, tracker *downloadTracker) ([]byte, error) {
	t, err := client.AddMagnet(magnetLink)
	if err != nil {
		return nil, fmt.Errorf("failed to add magnet: %w", err)
	}
	defer t.Drop()

	slog.Info("Waiting for torrent info", "torrent", torrentName)

	select {
	case <-t.GotInfo():
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// Build the expected file path within the torrent.
	// The server_path contains a server-side prefix (e.g., "g5/zlib1/zlib1/")
	// followed by the torrent directory name and the file within it.
	// We extract the path starting from the torrent base name.
	baseName := strings.TrimSuffix(torrentName, ".torrent")

	var searchPath string
	if idx := strings.Index(serverPath, baseName); idx >= 0 {
		searchPath = serverPath[idx:]
	} else {
		// Fallback: use the last path component
		parts := strings.Split(serverPath, "/")
		searchPath = parts[len(parts)-1]
	}

	slog.Info("Looking for file in torrent", "searchPath", searchPath)

	// Find the matching file in the torrent
	var targetFile *torrent.File
	for _, file := range t.Files() {
		if file.Path() == searchPath || strings.HasSuffix(file.Path(), "/"+searchPath) {
			targetFile = file
			break
		}
	}

	if targetFile == nil {
		return nil, fmt.Errorf("file not found in torrent: %s", searchPath)
	}

	slog.Info("Found file in torrent, downloading", "path", targetFile.Path(), "size", targetFile.Length())

	// Download only this file with highest priority
	targetFile.Download()
	tracker.update(0, targetFile.Length())

	// Wait for download to complete
	for targetFile.BytesCompleted() < targetFile.Length() {
		tracker.update(targetFile.BytesCompleted(), targetFile.Length())
		slog.Debug("Downloading file", "completed", targetFile.BytesCompleted(), "total", targetFile.Length())
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
			time.Sleep(500 * time.Millisecond)
		}
	}
	tracker.update(targetFile.Length(), targetFile.Length())

	slog.Info("File downloaded, reading contents", "path", targetFile.Path())

	reader := targetFile.NewReader()
	defer reader.Close()

	// We use a LimitReader because sometimes the torrent reader might read slightly past the file boundary
	// into padding bytes if the file ends in the middle of a piece.
	data, err := io.ReadAll(io.LimitReader(reader, targetFile.Length()))
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	slog.Info("File read into memory", "size", len(data))

	if EpubStorageDir != "" {
		if err := os.MkdirAll(EpubStorageDir, 0755); err != nil {
			slog.Warn("Failed to create storage directory", "dir", EpubStorageDir, "error", err)
		} else {
			path := filepath.Join(EpubStorageDir, outputFilename)
			if err := os.WriteFile(path, data, 0644); err != nil {
				slog.Warn("Failed to write file to storage", "path", path, "error", err)
			} else {
				slog.Info("File saved to storage", "path", path, "size", len(data))
			}
		}
	}

	return data, nil
}
