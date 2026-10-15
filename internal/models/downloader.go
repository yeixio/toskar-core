package models

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Downloader handles resumable HTTP downloads with integrity verification.
type Downloader struct {
	storage *Storage
	bus     *events.Bus
	db      *sql.DB
	client  *http.Client

	// QuotaLimitBytes returns the soft storage cap for models (0 = unlimited).
	QuotaLimitBytes func(ctx context.Context) (uint64, error)

	mu      sync.Mutex
	cancels map[string]context.CancelFunc
}

func NewDownloader(storage *Storage, bus *events.Bus, db *sql.DB) *Downloader {
	return &Downloader{
		storage: storage,
		bus:     bus,
		db:      db,
		client:  &http.Client{Timeout: 0},
		cancels: make(map[string]context.CancelFunc),
	}
}

// Download starts or resumes a model download. A vision model's projector
// is downloaded first, and alone when the model itself is already here.
func (d *Downloader) Download(ctx context.Context, entry CatalogEntry) error {
	if err := d.storage.EnsureDirs(); err != nil {
		return err
	}
	installed, finalPath, err := d.storage.IsInstalled(ctx, entry.ID)
	if err != nil {
		return err
	}
	var projector *ModelFile
	if p := entry.Projector; p != nil && !d.storage.HasProjector(entry.ID) {
		projector = p
	}
	if installed && projector == nil {
		return nil
	}

	// A vision model's size includes its projector.
	need := entry.SizeBytes
	if installed && projector != nil {
		need = projector.SizeBytes
	}
	if need == 0 {
		need = 1
	}
	if err := d.checkSpace(ctx, need); err != nil {
		return err
	}

	downloadID := uuid.NewString()
	dctx, cancel := context.WithCancel(ctx)
	d.mu.Lock()
	d.cancels[entry.ID] = cancel
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		delete(d.cancels, entry.ID)
		d.mu.Unlock()
	}()

	_, _ = d.db.ExecContext(ctx, `
		INSERT INTO downloads (id, model_id, status, temp_path, bytes_total)
		VALUES (?, ?, 'downloading', ?, ?)
		ON CONFLICT(id) DO NOTHING`,
		downloadID, entry.ID, d.storage.TempPath(entry.ID), need)

	d.bus.Publish(events.New(events.ModelDownloadStarted, map[string]any{
		"model_id": entry.ID,
	}))

	var done uint64
	if projector != nil {
		_, n, err := d.fetch(ctx, dctx, entry.ID, projector.URL, projector.SHA256,
			d.storage.ProjectorTempPath(entry.ID), d.storage.ProjectorPath(entry.ID), 0, need)
		if err != nil {
			return d.fail(ctx, entry.ID, downloadID, err)
		}
		done = n
	}
	if !installed {
		finalPath = d.storage.ModelPath(entry.ID)
		sum, n, err := d.fetch(ctx, dctx, entry.ID, entry.Source.URL, entry.Source.SHA256,
			d.storage.TempPath(entry.ID), finalPath, done, need)
		if err != nil {
			return d.fail(ctx, entry.ID, downloadID, err)
		}
		if err := d.storage.MarkInstalled(ctx, entry.ID, finalPath, sum, n); err != nil {
			return err
		}
	}
	_, _ = d.db.ExecContext(ctx, `UPDATE downloads SET status = 'completed', updated_at = datetime('now') WHERE model_id = ?`, entry.ID)
	d.bus.Publish(events.New(events.ModelDownloadCompleted, map[string]any{
		"model_id": entry.ID,
		"path":     finalPath,
	}))
	return nil
}

// fetch downloads url to finalPath, resuming tempPath, and checks its
// SHA-256 when one is given. Progress counts from base toward total, so a
// model and its projector show as one download.
func (d *Downloader) fetch(ctx, dctx context.Context, modelID, url, wantSum, tempPath, finalPath string, base, total uint64) (string, uint64, error) {
	if err := os.MkdirAll(filepath.Dir(tempPath), 0o755); err != nil {
		return "", 0, err
	}
	var offset int64
	if st, err := os.Stat(tempPath); err == nil {
		offset = st.Size()
	}

	req, err := http.NewRequestWithContext(dctx, http.MethodGet, url, nil)
	if err != nil {
		return "", 0, err
	}
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return "", 0, fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	flags := os.O_CREATE | os.O_WRONLY
	if offset > 0 && resp.StatusCode == http.StatusPartialContent {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
		offset = 0
	}
	if resp.ContentLength > 0 && base+uint64(resp.ContentLength)+uint64(offset) > total {
		total = base + uint64(resp.ContentLength) + uint64(offset)
	}

	f, err := os.OpenFile(tempPath, flags, 0o644)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()

	hasher := sha256.New()
	if offset > 0 {
		// Hash existing partial content.
		existing, err := os.ReadFile(tempPath)
		if err != nil {
			return "", 0, err
		}
		hasher.Write(existing)
	}

	buf := make([]byte, 32*1024)
	downloaded := uint64(offset)
	lastEmit := time.Now()

	for {
		select {
		case <-dctx.Done():
			return "", 0, dctx.Err()
		default:
		}
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return "", 0, werr
			}
			hasher.Write(buf[:n])
			downloaded += uint64(n)
			_, _ = d.db.ExecContext(ctx, `
				UPDATE downloads SET bytes_downloaded = ?, bytes_total = ?, updated_at = datetime('now') WHERE model_id = ?`,
				base+downloaded, total, modelID)
			if time.Since(lastEmit) > 250*time.Millisecond {
				d.emitProgress(modelID, base+downloaded, total)
				lastEmit = time.Now()
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", 0, readErr
		}
	}
	d.emitProgress(modelID, base+downloaded, total)

	sum := hex.EncodeToString(hasher.Sum(nil))
	if wantSum != "" && sum != wantSum {
		_ = os.Remove(tempPath)
		return "", 0, fmt.Errorf("sha256 mismatch: expected %s got %s", wantSum, sum)
	}
	if err := f.Close(); err != nil {
		return "", 0, err
	}
	if err := os.Rename(tempPath, finalPath); err != nil {
		return "", 0, err
	}
	return sum, downloaded, nil
}

func (d *Downloader) Cancel(modelID string) {
	d.mu.Lock()
	cancel, ok := d.cancels[modelID]
	d.mu.Unlock()
	if ok {
		cancel()
	}
}

func (d *Downloader) emitProgress(modelID string, downloaded, total uint64) {
	pct := float64(0)
	if total > 0 {
		pct = float64(downloaded) / float64(total) * 100
	}
	d.bus.Publish(events.New(events.ModelDownloadProgress, map[string]any{
		"model_id":         modelID,
		"bytes_downloaded": downloaded,
		"bytes_total":      total,
		"percent":          pct,
	}))
}

func (d *Downloader) fail(ctx context.Context, modelID, downloadID string, err error) error {
	_, _ = d.db.ExecContext(ctx, `UPDATE downloads SET status = 'failed', error = ?, updated_at = datetime('now') WHERE model_id = ?`,
		err.Error(), modelID)
	failure := contracts.FailureOf(err, "INSTALL_FAILED")
	d.bus.Publish(events.New(events.ModelDownloadFailed, map[string]any{
		"model_id":      modelID,
		"error":         failure.Text,
		"error_code":    failure.Code,
		"error_details": failure.Details,
	}))
	return err
}
