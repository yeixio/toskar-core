package models

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/gguf"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// ImportRequest adds a GGUF file on this computer as a model (#467).
type ImportRequest struct {
	// Path is the file, an absolute path ending in .gguf.
	Path string
	// InPlace uses the file where it is, without a copy: no doubled disk
	// use, but the model is gone if the file moves. Otherwise it's copied
	// into the models folder, and the original can be deleted.
	InPlace bool
	// ID, DisplayName, and Tags name it; empty, they come from the file.
	ID          string
	DisplayName string
	Tags        []string
}

// Imported is a model added from a file, with what its header says.
type Imported struct {
	ID      string
	Details gguf.Details
	// Copying is set while the file is copied in the background; its
	// progress comes as model.download events, as a download's does.
	Copying bool
	// Projector is a vision projector found beside the file, to offer as
	// its image support (see SetProjector).
	Projector string
}

// ImportFile adds a GGUF file on this computer as a model: checked first,
// then copied in the background or used in place. An upload's file, made
// for it, is moved in instead (see AdoptUpload).
func (m *Manager) ImportFile(ctx context.Context, req ImportRequest) (Imported, error) {
	path, err := importPath(req.Path)
	if err != nil {
		// A model another app keeps, such as an Ollama blob, has no .gguf
		// name; it's added when FindOtherApps lists it.
		p := filepath.Clean(strings.TrimSpace(req.Path))
		if !filepath.IsAbs(p) || !m.foundPath(ctx, p) {
			return Imported{}, err
		}
		path = p
	}
	return m.importChecked(ctx, req, path, false)
}

// AdoptUpload adds a file uploaded into the models folder's partial
// directory (see UploadPath) as a model, moving it into place.
func (m *Manager) AdoptUpload(ctx context.Context, req ImportRequest, uploaded string) (Imported, error) {
	rel, err := filepath.Rel(filepath.Join(m.storage.ModelsDir(), ".partial"), uploaded)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return Imported{}, fmt.Errorf("an upload must be in the models folder")
	}
	return m.importChecked(ctx, req, uploaded, true)
}

// UploadPath is where an upload is written before it's checked.
func (m *Manager) UploadPath() (string, error) {
	if err := m.storage.EnsureDirs(); err != nil {
		return "", err
	}
	dir := filepath.Join(m.storage.ModelsDir(), ".partial")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "upload-"+uuid.NewString()+".gguf"), nil
}

func (m *Manager) importChecked(ctx context.Context, req ImportRequest, path string, uploaded bool) (Imported, error) {
	st, err := os.Stat(path)
	if err != nil {
		return Imported{}, contracts.Errorf("MODEL_FILE_NOT_FOUND", map[string]any{"path": req.Path}, "no file at %s", req.Path)
	}
	if !st.Mode().IsRegular() {
		return Imported{}, contracts.Errorf("MODEL_FILE_INVALID", map[string]any{"detail": "not a file"}, "%s is not a file", req.Path)
	}
	details, err := gguf.Inspect(path)
	if err != nil {
		return Imported{}, contracts.Errorf("MODEL_FILE_INVALID", map[string]any{"detail": err.Error()}, "this isn't a model Toskar can use: %v", err)
	}
	if details.Projector() {
		return Imported{}, contracts.Errorf("MODEL_FILE_PROJECTOR", nil, "this is a vision projector, which helps a model see pictures; add the model it belongs to")
	}
	entry := importEntry(req, details, filepath.Base(path), uint64(st.Size()))
	// A copy of a catalog model, such as one another Toskar downloaded,
	// is that model: installed under its ID, with its name and abilities,
	// so it isn't offered for download again.
	match, matched := CatalogEntry{}, false
	if strings.TrimSpace(req.ID) == "" {
		match, matched = m.catalogMatch(ctx, details, path, uint64(st.Size()))
	}
	if matched {
		entry = match
	} else {
		entry.ID = m.freeID(ctx, entry.ID, path)
	}
	out := Imported{ID: entry.ID, Details: details}
	if !uploaded {
		out.Projector = projectorNear(path)
	}

	// A catalog model keeps its entry; it's stored, as installing one does.
	if !matched {
		m.catalog.Upsert(entry)
	}
	if err := m.storage.UpsertCatalogEntry(ctx, entry); err != nil {
		return out, err
	}
	switch {
	case uploaded:
		final := m.storage.ModelPath(entry.ID)
		sum, err := fileSHA256(path)
		if err != nil {
			return out, err
		}
		if err := os.Rename(path, final); err != nil {
			return out, err
		}
		if err := m.storage.MarkInstalled(ctx, entry.ID, final, sum, uint64(st.Size())); err != nil {
			return out, err
		}
	case req.InPlace:
		if err := m.storage.MarkInstalled(ctx, entry.ID, path, "", uint64(st.Size())); err != nil {
			return out, err
		}
		// Reading 20 GB to hash it takes a while; the model works meanwhile.
		go func(id string) {
			if sum, err := fileSHA256(path); err == nil {
				_ = m.storage.SetSHA256(context.Background(), id, sum)
			}
		}(entry.ID)
	default:
		if err := m.downloader.checkSpace(ctx, uint64(st.Size())); err != nil {
			return out, err
		}
		m.mu.Lock()
		m.downloading[entry.ID] = struct{}{}
		m.mu.Unlock()
		out.Copying = true
		go func() {
			defer func() {
				m.mu.Lock()
				delete(m.downloading, entry.ID)
				m.mu.Unlock()
			}()
			_ = m.downloader.CopyIn(context.Background(), entry.ID, path, uint64(st.Size()))
		}()
	}
	return out, nil
}

// catalogMatch is the catalog model a file is a copy of: named as Toskar
// saves a download (<id>.gguf) or as its source names it, of the same
// architecture and about its size, and not installed from another file. The catalog
// keeps no checksums, so the name and the header decide together.
func (m *Manager) catalogMatch(ctx context.Context, d gguf.Details, file string, size uint64) (CatalogEntry, bool) {
	filename := filepath.Base(file)
	stem := strings.TrimSuffix(filename, filepath.Ext(filename))
	for _, e := range m.catalog.List() {
		source := ""
		if u, err := url.Parse(e.Source.URL); err == nil && u.Path != "" {
			source = path.Base(u.Path)
		}
		if !strings.EqualFold(stem, e.ID) && (source == "" || !strings.EqualFold(filename, source)) {
			continue
		}
		if e.Training != nil && e.Training.Architecture != "" && d.Architecture != "" && !strings.EqualFold(e.Training.Architecture, d.Architecture) {
			continue
		}
		if e.SizeBytes > 0 && (size < e.SizeBytes*8/10 || size > e.SizeBytes*12/10) {
			continue
		}
		// Installed already, it's another copy, unless it's this same file.
		if installed, at, _ := m.storage.IsInstalled(ctx, e.ID); installed && at != file {
			continue
		}
		return e, true
	}
	return CatalogEntry{}, false
}

// importPath is req.Path, checked: absolute, and a .gguf file.
func importPath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" || !filepath.IsAbs(p) {
		return "", contracts.Errorf("MODEL_FILE_INVALID", map[string]any{"detail": "the path must be absolute"}, "give the file's full path")
	}
	if !strings.EqualFold(filepath.Ext(p), ".gguf") {
		return "", contracts.Errorf("MODEL_FILE_INVALID", map[string]any{"detail": "only .gguf files"}, "only .gguf model files can be added")
	}
	return filepath.Clean(p), nil
}

// importEntry is the catalog entry for an added file, from its header.
func importEntry(req ImportRequest, d gguf.Details, filename string, size uint64) CatalogEntry {
	name := strings.TrimSpace(req.DisplayName)
	if name == "" {
		name = d.Name
	}
	if name == "" {
		name = strings.TrimSuffix(filename, filepath.Ext(filename))
	}
	id := strings.TrimSpace(req.ID)
	if id == "" {
		base := name
		if d.Quantization != "" && !strings.Contains(strings.ToUpper(base), d.Quantization) {
			base += "-" + d.Quantization
		}
		id = deriveIDFromURL("", base+".gguf", "")
	}
	tags := req.Tags
	if len(tags) == 0 {
		tags = []string{"general"}
	}
	params := d.SizeLabel
	if params == "" && d.Parameters > 0 {
		params = parameterLabel(d.Parameters)
	}
	return CatalogEntry{
		ID:                id,
		DisplayName:       name,
		Summary:           "Added from a file",
		Family:            d.Architecture,
		Variant:           d.Quantization,
		Parameters:        params,
		SizeBytes:         size,
		MemoryNeededBytes: estimateMemory(size),
		Context:           int(min(d.ContextLength, 1<<31-1)),
		Source:            contracts.ModelSource{Format: "gguf"},
		Purpose:           []string{"general", "assistant"},
		Tags:              tags,
		Runtime:           []string{"llamacpp"},
		Dynamic:           true,
	}
}

// parameterLabel is a count as people write it: 8B, 1.5B, 350M.
func parameterLabel(n uint64) string {
	switch {
	case n >= 1e9:
		return strings.TrimSuffix(strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1e9), "0"), ".") + "B"
	case n >= 1e6:
		return fmt.Sprintf("%dM", n/1e6)
	}
	return fmt.Sprintf("%d", n)
}

// freeID is id, or id-2, id-3… when another model already has it, unless
// that model is this same file.
func (m *Manager) freeID(ctx context.Context, id, path string) string {
	if id == "" {
		id = "imported-model"
	}
	for n := 1; ; n++ {
		candidate := id
		if n > 1 {
			candidate = fmt.Sprintf("%s-%d", id, n)
		}
		_, inCatalog := m.catalog.Get(candidate)
		installed, at, _ := m.storage.IsInstalled(ctx, candidate)
		if (!inCatalog && !installed) || (installed && at == path) {
			return candidate
		}
	}
}

// checkSpace refuses work that would fill the disk or pass the model
// storage limit.
func (d *Downloader) checkSpace(ctx context.Context, need uint64) error {
	if need == 0 {
		need = 1
	}
	avail, err := d.storage.AvailableDiskBytes()
	if err != nil {
		return err
	}
	if avail < need {
		return fmt.Errorf("insufficient disk space: need %d bytes, have %d", need, avail)
	}
	if d.QuotaLimitBytes != nil {
		limit, err := d.QuotaLimitBytes(ctx)
		if err != nil {
			return err
		}
		if limit > 0 {
			used, err := d.storage.SumInstalledBytes(ctx)
			if err != nil {
				return err
			}
			if used+need > limit {
				return fmt.Errorf(
					"model storage limit reached: need %d more bytes, but only %d remain under your %d byte limit (using %d)",
					need, limit-used, limit, used,
				)
			}
		}
	}
	return nil
}

// CopyIn copies a file into the models folder as modelID, hashing it on
// the way, with the progress events a download sends.
func (d *Downloader) CopyIn(ctx context.Context, modelID, src string, size uint64) error {
	downloadID := uuid.NewString()
	temp := d.storage.TempPath(modelID)
	final := d.storage.ModelPath(modelID)
	_, _ = d.db.ExecContext(ctx, `
		INSERT INTO downloads (id, model_id, status, temp_path, bytes_total)
		VALUES (?, ?, 'downloading', ?, ?)
		ON CONFLICT(id) DO NOTHING`, downloadID, modelID, temp, size)
	d.bus.Publish(events.New(events.ModelDownloadStarted, map[string]any{"model_id": modelID}))
	cctx, cancel := context.WithCancel(ctx)
	d.mu.Lock()
	d.cancels[modelID] = cancel
	d.mu.Unlock()
	defer func() {
		cancel()
		d.mu.Lock()
		delete(d.cancels, modelID)
		d.mu.Unlock()
	}()
	sum, n, err := d.copyFile(cctx, modelID, src, temp, size)
	if err != nil {
		_ = os.Remove(temp)
		return d.fail(ctx, modelID, downloadID, err)
	}
	if err := os.Rename(temp, final); err != nil {
		return d.fail(ctx, modelID, downloadID, err)
	}
	if err := d.storage.MarkInstalled(ctx, modelID, final, sum, n); err != nil {
		return d.fail(ctx, modelID, downloadID, err)
	}
	_, _ = d.db.ExecContext(ctx, `UPDATE downloads SET status = 'completed', updated_at = datetime('now') WHERE model_id = ?`, modelID)
	d.bus.Publish(events.New(events.ModelDownloadCompleted, map[string]any{"model_id": modelID, "path": final}))
	return nil
}

func (d *Downloader) copyFile(ctx context.Context, modelID, src, temp string, size uint64) (string, uint64, error) {
	if err := os.MkdirAll(filepath.Dir(temp), 0o755); err != nil {
		return "", 0, err
	}
	in, err := os.Open(src)
	if err != nil {
		return "", 0, err
	}
	defer in.Close()
	out, err := os.Create(temp)
	if err != nil {
		return "", 0, err
	}
	defer out.Close()
	hasher := sha256.New()
	buf := make([]byte, 1<<20)
	var done uint64
	last := time.Now()
	for {
		if err := ctx.Err(); err != nil {
			return "", 0, err
		}
		n, rerr := in.Read(buf)
		if n > 0 {
			if _, err := out.Write(buf[:n]); err != nil {
				return "", 0, err
			}
			hasher.Write(buf[:n])
			done += uint64(n)
			if time.Since(last) > 250*time.Millisecond {
				d.emitProgress(modelID, done, size)
				last = time.Now()
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return "", 0, rerr
		}
	}
	d.emitProgress(modelID, done, size)
	if err := out.Close(); err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hasher.Sum(nil)), done, nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// editableTags are the tags a person can give a model they added: Auto
// sends coding requests to one tagged coding (#467). Vision comes with a
// projector, not a tag.
var editableTags = map[string]bool{"general": true, "coding": true, "reasoning": true, "writing": true}

// UpdateAdded renames or retags a model added from a file or a link; a
// catalog model keeps its own.
func (m *Manager) UpdateAdded(ctx context.Context, id string, displayName *string, tags []string) (CatalogEntry, error) {
	entry, ok := m.catalog.Get(id)
	if !ok {
		return CatalogEntry{}, contracts.Errorf("MODEL_NOT_FOUND", map[string]any{"model_id": id}, "no model %q", id)
	}
	if !entry.Dynamic {
		return CatalogEntry{}, contracts.Errorf("MODEL_NOT_EDITABLE", nil, "only a model you added can be renamed")
	}
	if displayName != nil {
		name := strings.TrimSpace(*displayName)
		if name == "" || len(name) > 120 {
			return CatalogEntry{}, contracts.Errorf("MODEL_NAME_INVALID", nil, "give the model a name of 1 to 120 characters")
		}
		entry.DisplayName = name
	}
	if tags != nil {
		kept := []string{}
		for _, t := range tags {
			if t = strings.ToLower(strings.TrimSpace(t)); editableTags[t] && !contains(kept, t) {
				kept = append(kept, t)
			}
		}
		if contains(entry.Tags, "vision") {
			kept = append(kept, "vision")
		}
		if len(kept) == 0 {
			kept = []string{"general"}
		}
		entry.Tags = kept
	}
	m.catalog.Upsert(entry)
	return entry, m.storage.UpsertCatalogEntry(ctx, entry)
}

// SetProjector gives a model its vision projector (llama.cpp's mmproj), a
// GGUF file on this computer, so it can see pictures (#467). The file is
// copied beside the model.
func (m *Manager) SetProjector(ctx context.Context, id, path string) error {
	entry, ok := m.catalog.Get(id)
	if !ok {
		return contracts.Errorf("MODEL_NOT_FOUND", map[string]any{"model_id": id}, "no model %q", id)
	}
	if installed, _, err := m.storage.IsInstalled(ctx, id); err != nil || !installed {
		return contracts.Errorf("MODEL_NOT_INSTALLED", map[string]any{"model_id": id}, "model %q not installed", id)
	}
	p := filepath.Clean(strings.TrimSpace(path))
	if !filepath.IsAbs(p) {
		return contracts.Errorf("MODEL_FILE_INVALID", map[string]any{"detail": "the path must be absolute"}, "give the file's full path")
	}
	if !strings.EqualFold(filepath.Ext(p), ".gguf") && !m.foundProjector(ctx, p) {
		return contracts.Errorf("MODEL_FILE_INVALID", map[string]any{"detail": "only .gguf files"}, "only .gguf files can be added")
	}
	d, err := gguf.Inspect(p)
	if err != nil {
		return contracts.Errorf("MODEL_FILE_INVALID", map[string]any{"detail": err.Error()}, "this isn't a file Toskar can use: %v", err)
	}
	if !d.Projector() {
		return contracts.Errorf("MODEL_FILE_NOT_PROJECTOR", nil, "this is a model, not a vision projector")
	}
	temp := m.storage.ProjectorTempPath(id)
	if err := os.MkdirAll(filepath.Dir(temp), 0o755); err != nil {
		return err
	}
	if err := copyPlain(p, temp); err != nil {
		_ = os.Remove(temp)
		return err
	}
	if err := os.Rename(temp, m.storage.ProjectorPath(id)); err != nil {
		return err
	}
	st, _ := os.Stat(m.storage.ProjectorPath(id))
	size := uint64(0)
	if st != nil {
		size = uint64(st.Size())
	}
	entry.Projector = &ModelFile{SizeBytes: size}
	entry.Capabilities.Vision = true
	if !contains(entry.Tags, "vision") {
		entry.Tags = append(entry.Tags, "vision")
	}
	m.catalog.Upsert(entry)
	return m.storage.UpsertCatalogEntry(ctx, entry)
}

// projectorNear is a vision projector in the same folder as a model file,
// the way LM Studio and Hugging Face keep them, or "".
func projectorNear(model string) string {
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(model), "*.gguf"))
	for _, p := range matches {
		if p == model || !strings.Contains(strings.ToLower(filepath.Base(p)), "mmproj") {
			continue
		}
		if d, err := gguf.Inspect(p); err == nil && d.Projector() {
			return p
		}
	}
	return ""
}

func copyPlain(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
