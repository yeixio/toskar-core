package training

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Export states.
const (
	ExportNone      = "none"
	ExportExporting = "exporting"
	ExportReady     = "ready"
	ExportFailed    = "failed"
)

// Export events.
const (
	EventExportCompleted = "training.export.completed"
	EventExportFailed    = "training.export.failed"
)

// ExportStatus describes a revision merged into a standalone GGUF: one file
// that llama.cpp, LM Studio, Ollama, and other GGUF tools load without the
// adapter.
type ExportStatus struct {
	AIID     string `json:"ai_id"`
	Revision int    `json:"revision"`
	State    string `json:"state"`
	Filename string `json:"filename,omitempty"`
	// SizeBytes is the file's size when ready, and the estimate otherwise.
	SizeBytes uint64     `json:"size_bytes,omitempty"`
	Error     string     `json:"error,omitempty"`
	CreatedAt *time.Time `json:"created_at,omitempty"`
	// Instructions are not part of the file. Another tool needs them as its
	// system prompt for the model to behave as the AI does here.
	Instructions string `json:"instructions,omitempty"`
}

// exportRun is a merge in progress, or one that failed.
type exportRun struct {
	cancel context.CancelFunc
	done   bool
	err    error
	size   uint64
}

func exportKey(aiID string, revision int) string { return AdapterID(aiID, revision) }

func (s *Service) exportsDir(aiID string) string { return filepath.Join(s.d.DataDir, "exports", aiID) }

func (s *Service) exportPath(ai SpecializedAI, revision int) string {
	return filepath.Join(s.exportsDir(ai.ID), fmt.Sprintf("%s-r%d.gguf", ai.Slug, revision))
}

// exportable returns the AI and revision when the revision's adapter is on
// this computer.
func (s *Service) exportable(ctx context.Context, aiID string, revision int) (SpecializedAI, Revision, error) {
	ai, err := s.d.Repo.GetAI(ctx, aiID)
	if err != nil {
		return SpecializedAI{}, Revision{}, err
	}
	rev, err := s.d.Repo.GetRevision(ctx, aiID, revision)
	if err != nil {
		return SpecializedAI{}, Revision{}, err
	}
	if _, err := os.Stat(rev.adapterPath); err != nil {
		return SpecializedAI{}, Revision{}, fmt.Errorf("revision %d's trained adapter is not on this computer", revision)
	}
	return ai, rev, nil
}

// ExportStatus reports whether a revision has been merged into a GGUF.
func (s *Service) ExportStatus(ctx context.Context, aiID string, revision int) (ExportStatus, error) {
	ai, _, err := s.exportable(ctx, aiID, revision)
	if err != nil {
		return ExportStatus{}, err
	}
	return s.exportStatus(ai, revision), nil
}

func (s *Service) exportStatus(ai SpecializedAI, revision int) ExportStatus {
	st := ExportStatus{AIID: ai.ID, Revision: revision, State: ExportNone, Instructions: ai.Instructions}
	path := s.exportPath(ai, revision)
	var run exportRun
	s.mu.Lock()
	r, found := s.exports[exportKey(ai.ID, revision)]
	if found {
		run = *r
	}
	s.mu.Unlock()
	switch {
	case found && !run.done:
		st.State, st.SizeBytes = ExportExporting, run.size
		return st
	case found && run.err != nil:
		st.State, st.Error = ExportFailed, run.err.Error()
		return st
	}
	if info, err := os.Stat(path); err == nil {
		t := info.ModTime().UTC()
		st.State, st.Filename, st.SizeBytes, st.CreatedAt = ExportReady, filepath.Base(path), uint64(info.Size()), &t
	}
	return st
}

// Export merges a revision's adapter into its base model and writes a
// standalone GGUF. It returns at once; the merge runs in the background
// (a few seconds for a small model, a minute or two for a large one).
func (s *Service) Export(ctx context.Context, aiID string, revision int) (ExportStatus, error) {
	ai, rev, err := s.exportable(ctx, aiID, revision)
	if err != nil {
		return ExportStatus{}, err
	}
	if st := s.exportStatus(ai, revision); st.State == ExportReady || st.State == ExportExporting {
		return st, nil
	}
	if s.d.ExportTool == nil || s.d.ModelPath == nil {
		return ExportStatus{}, fmt.Errorf("exporting is not available in this build")
	}
	tool, err := s.d.ExportTool(ctx)
	if err != nil {
		return ExportStatus{}, err
	}
	base, err := s.d.ModelPath(ctx, rev.BaseModelID)
	if err != nil {
		return ExportStatus{}, fmt.Errorf("the base model %s is not installed on this computer: %w", rev.BaseModelID, err)
	}
	baseInfo, err := os.Stat(base)
	if err != nil {
		return ExportStatus{}, fmt.Errorf("the base model %s is not installed on this computer", rev.BaseModelID)
	}
	tensors, err := readGGUFTensors(rev.adapterPath)
	if err != nil {
		return ExportStatus{}, err
	}
	size := uint64(baseInfo.Size()) + mergedGrowth(tensors)
	dir := s.exportsDir(ai.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ExportStatus{}, err
	}
	if s.d.FreeDisk != nil {
		if free, err := s.d.FreeDisk(dir); err == nil && free < size {
			return ExportStatus{}, fmt.Errorf("the exported model needs about %s of free disk space, and %s is free", gib(size), gib(free))
		}
	}

	key := exportKey(ai.ID, revision)
	s.mu.Lock()
	if run := s.exports[key]; run != nil && !run.done {
		s.mu.Unlock()
		return s.exportStatus(ai, revision), nil
	}
	runCtx, cancel := context.WithCancel(context.Background())
	run := &exportRun{cancel: cancel, size: size}
	s.exports[key] = run
	s.mu.Unlock()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer cancel()
		err := s.merge(runCtx, tool, base, rev.adapterPath, s.exportPath(ai, revision))
		s.mu.Lock()
		run.done, run.err = true, err
		if err == nil && s.exports[key] == run {
			delete(s.exports, key)
		}
		s.mu.Unlock()
		payload := map[string]any{"ai_id": ai.ID, "name": ai.Name, "revision": revision}
		switch {
		case err == nil:
			s.d.Publish(EventExportCompleted, payload)
		case runCtx.Err() == nil:
			s.d.Logger.Warn("export failed", "ai", ai.ID, "revision", revision, "error", err)
			payload["error"] = err.Error()
			s.d.Publish(EventExportFailed, payload)
		}
	}()
	return s.exportStatus(ai, revision), nil
}

// merge runs llama-export-lora. The file appears under its final name only
// when it is complete.
func (s *Service) merge(ctx context.Context, tool, base, adapter, out string) error {
	partial := out + ".partial"
	defer os.Remove(partial)
	cmd := exec.CommandContext(ctx, tool, "-m", base, "--lora", adapter, "-o", partial)
	cmd.Dir = filepath.Dir(tool)
	var output tailBuffer
	cmd.Stdout, cmd.Stderr = &output, &output
	setProcessGroup(cmd)
	cmd.Cancel = func() error {
		stopProcessGroup(cmd, 2*time.Second)
		return nil
	}
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ErrCancelled
		}
		return fmt.Errorf("llama-export-lora: %w: %s", err, strings.TrimSpace(output.String()))
	}
	if ctx.Err() != nil {
		return ErrCancelled
	}
	return os.Rename(partial, out)
}

// ExportFile returns the path and download name of a finished export.
func (s *Service) ExportFile(ctx context.Context, aiID string, revision int) (string, string, error) {
	ai, err := s.d.Repo.GetAI(ctx, aiID)
	if err != nil {
		return "", "", err
	}
	path := s.exportPath(ai, revision)
	if _, err := os.Stat(path); err != nil {
		return "", "", fmt.Errorf("revision %d has not been exported: %w", revision, ErrNotFound)
	}
	return path, filepath.Base(path), nil
}

// DeleteExport stops a merge in progress and removes the exported file.
func (s *Service) DeleteExport(ctx context.Context, aiID string, revision int) error {
	ai, err := s.d.Repo.GetAI(ctx, aiID)
	if err != nil {
		return err
	}
	s.cancelExports(aiID, revision)
	if err := os.Remove(s.exportPath(ai, revision)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// cancelExports stops and forgets an AI's merges: one revision, or all when
// revision is 0.
func (s *Service) cancelExports(aiID string, revision int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, run := range s.exports {
		if key == exportKey(aiID, revision) || (revision == 0 && strings.HasPrefix(key, aiID+"@")) {
			run.cancel()
			delete(s.exports, key)
		}
	}
}

// tailBuffer keeps the last few KB of a process's output for error messages.
type tailBuffer struct{ b []byte }

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.b = append(t.b, p...)
	if len(t.b) > 4096 {
		t.b = t.b[len(t.b)-4096:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	s := string(t.b)
	// The last line usually says what went wrong.
	if i := strings.LastIndex(strings.TrimSpace(s), "\n"); i >= 0 {
		return strings.TrimSpace(s[i:])
	}
	return s
}
