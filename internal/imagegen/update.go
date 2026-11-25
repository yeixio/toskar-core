package imagegen

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// Each Release installs into a folder of its own, so a new pin leaves a
// computer set up with an earlier one without a program for it. Its models
// still work with the new program, so the first picture or clip after an
// update installs it (about 35 MB) and carries on, instead of asking the
// person to set everything up again (#548).

// earlierReleases are the folders of other releases set up here.
func (s *Setup) earlierReleases() []string {
	entries, err := os.ReadDir(s.ProgramDir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() || e.Name() == Release {
			continue
		}
		if _, err := os.Stat(filepath.Join(s.ProgramDir, e.Name(), "cli")); err == nil {
			out = append(out, filepath.Join(s.ProgramDir, e.Name()))
		}
	}
	return out
}

// stale reports a setup made with an earlier release: a model is here,
// and an earlier release's program, but not this release's.
func (s *Setup) stale() bool {
	return s.CLI() == "" && s.ActiveModel() != "" && len(s.earlierReleases()) > 0
}

// update installs this release's program for a stale setup, the same kind
// of build as before, and removes the earlier ones.
func (s *Setup) update(ctx context.Context) error {
	archive, ok := s.platform()
	if !ok {
		return fmt.Errorf("stable-diffusion.cpp has no build for this kind of computer")
	}
	if err := s.installProgram(ctx, archive, func(int64) {}); err != nil {
		return fmt.Errorf("stable-diffusion.cpp could not be updated: %w", err)
	}
	for _, dir := range s.earlierReleases() {
		_ = os.RemoveAll(dir)
	}
	if s.Updated != nil {
		s.Updated(archive.Name)
	}
	if s.Changed != nil {
		s.Changed()
	}
	return nil
}

// ensureCurrent updates a stale setup before a job.
func (e *Engine) ensureCurrent(ctx context.Context) error {
	if !e.Setup.stale() {
		return nil
	}
	if e.Logger != nil {
		e.Logger.Info("updating stable-diffusion.cpp", "release", Release)
	}
	return e.Setup.update(ctx)
}
