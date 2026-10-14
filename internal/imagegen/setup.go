package imagegen

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Setup installs stable-diffusion.cpp and image models, and knows which
// are ready.
type Setup struct {
	// ProgramDir holds the stable-diffusion.cpp builds (runtimes/sdcpp).
	ProgramDir string
	// ModelsDir holds one folder per image model (models/images).
	ModelsDir string
	HTTP      *http.Client
	// Memory returns this computer's memory, for the recommendation.
	Memory func() int64
	// FreeBytes returns the free space where models are kept; nil skips the
	// check.
	FreeBytes func() (int64, error)
	// Sandboxed refuses setup: the Mac App Store build cannot run a program
	// it downloads.
	Sandboxed bool
	// Changed is called when what is installed changes: a setup ended or a
	// model was removed.
	Changed func()
	// Catalog lists the models this setup offers; nil is the image models.
	Catalog func() []Model
	// Graphics is the memory this computer's graphics can use: a card's
	// own, or the share of system memory Apple silicon gives its graphics;
	// 0 when there are none or it isn't known (#550).
	Graphics func() int64
	// Shared reports graphics that share system memory (Apple silicon),
	// so Graphics is a share of Memory rather than a card's own.
	Shared func() bool
	// Token is the person's Hugging Face token, for files behind a
	// model's license (#551); "" when none is set.
	Token func() string
	// GPU reports whether the Vulkan build would use a GPU here (#154);
	// nil means it wouldn't, and the CPU build is used.
	GPU func() bool
	// archive overrides this platform's build, and builds its choice of
	// builds; tests set them.
	archive *Archive
	builds  []Archive
	// fileURL overrides where model files come from; tests set it.
	fileURL func(File) string

	mu     sync.Mutex
	job    *job
	cancel context.CancelFunc
	// tightOK are models the person agreed to set up though they would
	// be slow or near the limit here.
	tightOK map[string]bool
}

type job struct {
	model   string
	stage   string
	total   int64
	done    atomic.Int64
	running bool
	err     string
}

// Job is the setup in progress, or the last one.
type Job struct {
	ModelID string `json:"model_id"`
	// Stage is program or model.
	Stage string `json:"stage"`
	Done  int64  `json:"done_bytes"`
	Total int64  `json:"total_bytes"`
	// Running is false once it finished or failed.
	Running bool   `json:"running"`
	Error   string `json:"error,omitempty"`
}

// ModelStatus is a catalog model and whether it is here.
type ModelStatus struct {
	Model
	SizeBytes   int64 `json:"size_bytes"`
	Installed   bool  `json:"installed"`
	Recommended bool  `json:"recommended"`
	// TooLittleMemory is a model this computer can't hold: it isn't
	// offered, set up, or run here. TightMemory is one it can, below the
	// memory it's comfortable with, so it may be slow or fail.
	TooLittleMemory bool `json:"too_little_memory,omitempty"`
	TightMemory     bool `json:"tight_memory,omitempty"`
	// LicenseAccepted is a model behind a license that the person has
	// confirmed their license covers (#551).
	LicenseAccepted bool `json:"license_accepted,omitempty"`
	// GraphicsShort is a model this computer's own graphics card has less
	// memory for than it's comfortable with: parts of it run from system
	// memory, more slowly (#550).
	GraphicsShort bool `json:"graphics_short,omitempty"`
	// Verdict is works, tight (it runs, slowly or near the limit, and is
	// set up only when the person agrees), or unsupported (#550).
	Verdict string `json:"verdict"`
	// Short is what a tight or unsupported verdict is short of: memory,
	// graphics (a card's own memory), or shared (the share of system
	// memory the graphics may use); NeedBytes is what the model needs of
	// it, and HaveBytes what this computer has, for "needs 14 GB; this
	// computer has 10 GB" (#550).
	Short     string `json:"short,omitempty"`
	NeedBytes int64  `json:"need_bytes,omitempty"`
	HaveBytes int64  `json:"have_bytes,omitempty"`
}

// What a verdict is short of (#550).
const (
	ShortMemory   = "memory"
	ShortGraphics = "graphics"
	ShortShared   = "shared"
)

// shortOf is what m is short of on a computer with this memory and
// graphics memory, with what it needs and what there is; "" when nothing.
func shortOf(m Model, memory, graphics int64, shared bool) (short string, need, have int64) {
	switch {
	case tooLittleMemory(m, memory):
		return ShortMemory, m.SizeBytes() + headroom, memory
	case tightMemory(m, memory):
		return ShortMemory, m.MemoryBytes, memory
	case graphicsShort(m, graphics):
		if shared {
			return ShortShared, m.GraphicsBytes, graphics
		}
		return ShortGraphics, m.GraphicsBytes, graphics
	}
	return "", 0, 0
}

// Verdicts (#550).
const (
	VerdictWorks       = "works"
	VerdictTight       = "tight"
	VerdictUnsupported = "unsupported"
)

// graphicsShort reports a graphics card with less memory than m is
// comfortable with. Without a card of its own, memory decides.
func graphicsShort(m Model, graphics int64) bool {
	return graphics > 0 && m.GraphicsBytes > 0 && graphics < m.GraphicsBytes
}

// verdict sums up whether m runs on a computer with this memory and
// graphics memory.
func verdict(m Model, memory, graphics int64) string {
	switch {
	case tooLittleMemory(m, memory):
		return VerdictUnsupported
	case tightMemory(m, memory) || graphicsShort(m, graphics):
		return VerdictTight
	}
	return VerdictWorks
}

// headroom is memory beyond a model's files that making a picture needs.
const headroom = 2 << 30

// tooLittleMemory reports a computer that can't hold m's files and what
// running them needs; it would fail, or freeze the computer swapping.
func tooLittleMemory(m Model, memory int64) bool {
	return memory > 0 && memory < m.SizeBytes()+headroom
}

// tightMemory reports a computer that can hold m, below what it's
// comfortable with (MemoryBytes).
func tightMemory(m Model, memory int64) bool {
	return memory > 0 && memory < m.MemoryBytes && !tooLittleMemory(m, memory)
}

// memoryShort says why a model can't run on a computer with memory.
func memoryShort(m Model, memory int64) string {
	return fmt.Sprintf("%s needs at least %.0f GB of memory, and this computer has %.0f GB, so it can't run here; a paired computer with more memory can", m.Name, gb(m.SizeBytes()+headroom), gb(memory))
}

// Accelerated reports whether pictures and clips are made on the GPU here:
// the installed build, or the one setup would install (#154). Without it a
// picture takes minutes and a clip can take most of an hour.
func (s *Setup) Accelerated() bool {
	if s.CLI() != "" {
		return gpuBuild(s.InstalledBuild())
	}
	a, ok := s.platform()
	return ok && gpuBuild(a.Build)
}

// InstalledBuild is the build of the installed stable-diffusion.cpp: metal,
// vulkan, or cpu, or "" when none is. One installed before builds were
// recorded is Metal on a Mac and the CPU build elsewhere.
func (s *Setup) InstalledBuild() string {
	if s.CLI() == "" {
		return ""
	}
	if raw, err := os.ReadFile(filepath.Join(s.ProgramDir, Release, "build")); err == nil {
		if b := strings.TrimSpace(string(raw)); b != "" {
			return b
		}
	}
	if runtime.GOOS == "darwin" {
		return BuildMetal
	}
	return BuildCPU
}

// preference is the build the person chose: cpu, or "" for the GPU build
// when one runs here.
func (s *Setup) preference() string {
	raw, _ := os.ReadFile(filepath.Join(s.ProgramDir, "prefer"))
	return strings.TrimSpace(string(raw))
}

// gpuBuildHere is the GPU build this computer could use, or "".
func (s *Setup) gpuBuildHere() string {
	if s.archive != nil {
		return ""
	}
	if a, ok := pickArchive(s.archives(), s.GPU, ""); ok && gpuBuild(a.Build) {
		return a.Build
	}
	return ""
}

// Status is what is set up.
type Status struct {
	// Supported is false where there is no build or setup cannot run;
	// Unsupported says why.
	Supported   bool   `json:"supported"`
	Unsupported string `json:"unsupported,omitempty"`
	// Ready means images can be made now.
	Ready bool `json:"ready"`
	// Program reports stable-diffusion.cpp installed.
	Program bool   `json:"program"`
	Release string `json:"release"`
	// Active is the model used for images.
	Active string `json:"active,omitempty"`
	// MemoryBytes is this computer's memory, and Accelerated whether its
	// build uses the GPU: without it, each picture or clip takes minutes.
	MemoryBytes int64 `json:"memory_bytes,omitempty"`
	Accelerated bool  `json:"accelerated"`
	// GraphicsBytes is the memory this computer's graphics can use, and
	// SharedGraphics says it's a share of MemoryBytes (#550).
	GraphicsBytes  int64 `json:"graphics_bytes,omitempty"`
	SharedGraphics bool  `json:"shared_graphics,omitempty"`
	// Build is the installed build (metal, vulkan, or cpu), and GPUBuild
	// the GPU build this computer can use, or "" (#154).
	Build    string        `json:"build,omitempty"`
	GPUBuild string        `json:"gpu_build,omitempty"`
	Models   []ModelStatus `json:"models"`
	Job      *Job          `json:"job,omitempty"`
	// TokenSet reports a Hugging Face token set for models behind a
	// license (#551).
	TokenSet bool `json:"huggingface_token_set,omitempty"`
	// Elsewhere are paired computers a model works well on, for a model
	// that's tight or too large here (#550). The app fills it in.
	Elsewhere []Place `json:"elsewhere,omitempty"`
}

// Place is a paired computer a model works well on, and whether it's set
// up there already.
type Place struct {
	NodeID    string `json:"node_id"`
	NodeName  string `json:"node_name"`
	ModelID   string `json:"model_id"`
	Installed bool   `json:"installed,omitempty"`
}

func (s *Setup) models() []Model {
	if s.Catalog != nil {
		return s.Catalog()
	}
	return Catalog()
}

func (s *Setup) lookup(id string) (Model, bool) { return lookupIn(s.models(), id) }

// memory is this computer's memory, or 0 when it isn't known.
func (s *Setup) memory() int64 {
	if s.Memory == nil {
		return 0
	}
	return s.Memory()
}

// programMu keeps two setups, such as images and video, from installing
// the same stable-diffusion.cpp at once.
var programMu sync.Mutex

func (s *Setup) platform() (Archive, bool) {
	if s.archive != nil {
		return *s.archive, true
	}
	return pickArchive(s.archives(), s.GPU, s.preference())
}

// archives are this computer's builds, the GPU build first.
func (s *Setup) archives() []Archive {
	if s.builds != nil {
		return s.builds
	}
	return platformArchives()
}

func (s *Setup) unsupported() string {
	if s.Sandboxed {
		return "this copy of Toskar runs in the macOS App Sandbox, which cannot run a program it downloads"
	}
	if _, ok := s.platform(); !ok {
		return "stable-diffusion.cpp has no build for this kind of computer"
	}
	return ""
}

// Status reports what is installed and any setup in progress.
func (s *Setup) Status() Status {
	why := s.unsupported()
	st := Status{Supported: why == "", Unsupported: why, Release: Release, Program: s.CLI() != "", Active: s.ActiveModel()}
	// A setup from an earlier release counts as ready: its program is
	// updated before the next job (update.go).
	st.Ready = st.Supported && (st.Program || s.stale()) && st.Active != ""
	memory := s.memory()
	st.MemoryBytes, st.Accelerated = memory, s.Accelerated()
	st.Build, st.GPUBuild = s.InstalledBuild(), s.gpuBuildHere()
	graphics, shared := s.graphics(), s.shared()
	st.GraphicsBytes, st.SharedGraphics = graphics, shared
	rec := recommendIn(s.models(), memory).ID
	for _, m := range s.models() {
		short, need, have := shortOf(m, memory, graphics, shared)
		st.Models = append(st.Models, ModelStatus{Model: m, SizeBytes: m.SizeBytes(), Installed: s.installed(m.ID), Recommended: m.ID == rec,
			TooLittleMemory: tooLittleMemory(m, memory), TightMemory: tightMemory(m, memory), LicenseAccepted: m.NeedsLicense && s.licenseAccepted(m.ID),
			GraphicsShort: graphicsShort(m, graphics), Verdict: verdict(m, memory, graphics),
			Short: short, NeedBytes: need, HaveBytes: have})
	}
	st.TokenSet = s.token() != ""
	s.mu.Lock()
	if j := s.job; j != nil {
		st.Job = &Job{ModelID: j.model, Stage: j.stage, Done: j.done.Load(), Total: j.total, Running: j.running, Error: j.err}
	}
	s.mu.Unlock()
	return st
}

// CLI returns the installed sd-cli, or "".
func (s *Setup) CLI() string {
	raw, err := os.ReadFile(filepath.Join(s.ProgramDir, Release, "cli"))
	if err != nil {
		return ""
	}
	path := filepath.Join(s.ProgramDir, Release, strings.TrimSpace(string(raw)))
	if st, err := os.Stat(path); err != nil || st.IsDir() {
		return ""
	}
	return path
}

func (s *Setup) modelDir(id string) string { return filepath.Join(s.ModelsDir, id) }

func (s *Setup) installed(id string) bool {
	_, err := os.Stat(filepath.Join(s.modelDir(id), "ready"))
	return err == nil
}

// ActiveModel is the model images are made with: the one chosen last, or
// the first installed one.
func (s *Setup) ActiveModel() string {
	if raw, err := os.ReadFile(filepath.Join(s.ModelsDir, "active")); err == nil {
		if id := strings.TrimSpace(string(raw)); s.installed(id) && !s.editOnly(id) {
			return id
		}
	}
	for _, m := range s.models() {
		if s.installed(m.ID) && !m.EditOnly {
			return m.ID
		}
	}
	return ""
}

// editOnly reports a model that only edits, which is never the active one.
func (s *Setup) editOnly(id string) bool {
	m, ok := s.lookup(id)
	return ok && m.EditOnly
}

// canEdit reports an installed model that can edit pictures.
func (s *Setup) canEdit() bool {
	for _, m := range s.models() {
		if m.Edits && s.installed(m.ID) {
			return true
		}
	}
	return false
}

// editModelMissing says what to set up to edit pictures.
func editModelMissing(s *Setup) string {
	name := ""
	for _, m := range s.models() {
		if m.Edits && !m.Untested && !tooLittleMemory(m, s.memory()) {
			name = m.Name
			break
		}
	}
	active := "The model set up here"
	if m, ok := s.lookup(s.ActiveModel()); ok {
		active = m.Name
	}
	video := false
	if list := s.models(); len(list) > 0 && list[0].Kind == "video" {
		video = true
	}
	// None that takes a picture fits this computer: a paired computer
	// with more memory can.
	switch {
	case name == "" && video:
		return fmt.Sprintf("%s can't bring pictures to life, and no model that can fits this computer; a paired computer with more memory can.", active)
	case name == "":
		return fmt.Sprintf("%s can't edit pictures, and no model that can fits this computer; a paired computer with more memory can.", active)
	case video:
		return fmt.Sprintf("%s can't bring pictures to life. Set up %s on the Tools page for clips from pictures.", active, name)
	}
	return fmt.Sprintf("%s can't edit pictures. Set up %s on the Tools page to edit them.", active, name)
}

// pathsFor returns the model for a job and its files by role: the active
// model, or for an edit the active model can't make, an installed one that
// can (#548).
//
// For a new picture or clip at a quality, it's the installed model of that
// tier, or the active model when none is, with the name of the catalog
// model that would give it, so the person can be told (#549). Edits and
// clips from pictures keep their own routing.
func (s *Setup) pathsFor(edit bool, quality string) (Model, map[string]string, string, error) {
	id := s.ActiveModel()
	m, ok := s.lookup(id)
	if !ok {
		return Model{}, nil, "", errors.New("no model is installed")
	}
	missing := ""
	if quality != "" && !edit && m.Tier != quality {
		found := false
		for _, other := range s.models() {
			if other.Tier == quality && !other.EditOnly && s.installed(other.ID) {
				m, id, found = other, other.ID, true
				break
			}
		}
		if !found {
			for _, other := range s.models() {
				if other.Tier == quality && !other.EditOnly {
					missing = other.Name
					break
				}
			}
		}
	}
	if edit {
		// A model made for editing comes first, then the active model if
		// it edits, then any installed model that edits.
		for _, other := range s.models() {
			if other.EditOnly && s.installed(other.ID) {
				m, id = other, other.ID
				break
			}
		}
		if !m.Edits {
			for _, other := range s.models() {
				if other.Edits && s.installed(other.ID) {
					m, id = other, other.ID
					break
				}
			}
		}
	}
	out := map[string]string{}
	for _, f := range m.Files {
		out[f.Role] = filepath.Join(s.modelDir(id), filepath.Base(f.Path))
	}
	return m, out, missing, nil
}

// Start installs what the model needs, in the background. Status reports
// its progress.
func (s *Setup) Start(id string) error {
	if why := s.unsupported(); why != "" {
		return errors.New(why)
	}
	m, ok := s.lookup(id)
	if !ok {
		return fmt.Errorf("unknown model %q", id)
	}
	// A model behind a license needs the person's token and their word
	// that their license covers their use (#551).
	if m.NeedsLicense && !s.installed(id) {
		if s.token() == "" {
			return fmt.Errorf("%s needs your own Hugging Face token: add it in Settings, after accepting the license at %s", m.Name, m.LicenseURL)
		}
		if !s.licenseAccepted(id) {
			return fmt.Errorf("confirm that your license for %s covers how you'll use it before setting it up", m.Name)
		}
	}
	if memory := s.memory(); tooLittleMemory(m, memory) {
		return errors.New(memoryShort(m, memory))
	}
	// A model that would be slow or near the limit here is set up only
	// when the person agrees (#550).
	if !s.installed(id) && verdict(m, s.memory(), s.graphics()) == VerdictTight && !s.tightAllowed(id) {
		return errors.New(tightReason(m, s.memory(), s.graphics(), s.shared()))
	}
	need := int64(0)
	if !s.installed(id) {
		need = m.SizeBytes()
	}
	archive, _ := s.platform()
	program := s.CLI() == ""
	if program {
		need += archive.Size * 3 // the archive and what it unpacks to
	}
	if s.FreeBytes != nil && need > 0 {
		// 0 means the space could not be measured.
		if free, err := s.FreeBytes(); err == nil && free > 0 && free < need+(1<<30) {
			return fmt.Errorf("setting up %s needs %.1f GB of free space; %.1f GB is free", m.Name, gb(need+(1<<30)), gb(free))
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.job != nil && s.job.running {
		return errors.New("image generation is already being set up")
	}
	j := &job{model: id, running: true}
	if program {
		j.total += archive.Size
	}
	if !s.installed(id) {
		j.total += m.SizeBytes()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.job, s.cancel = j, cancel
	go s.run(ctx, j, m, program, archive)
	return nil
}

func gb(n int64) float64 { return float64(n) / (1 << 30) }

func (s *Setup) run(ctx context.Context, j *job, m Model, program bool, archive Archive) {
	err := s.install(ctx, j, m, program, archive)
	if s.Changed != nil {
		defer s.Changed()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	j.running = false
	switch {
	case errors.Is(err, context.Canceled):
		j.err = "Setup was stopped. Starting it again picks up where it left off."
	case err != nil:
		j.err = err.Error()
	}
	s.cancel = nil
}

func (s *Setup) setStage(j *job, stage string) {
	s.mu.Lock()
	j.stage = stage
	s.mu.Unlock()
}

func (s *Setup) client() *http.Client {
	if s.HTTP != nil {
		return s.HTTP
	}
	return http.DefaultClient
}

func (s *Setup) install(ctx context.Context, j *job, m Model, program bool, archive Archive) error {
	add := func(n int64) { j.done.Add(n) }
	if program {
		s.setStage(j, "program")
		if err := s.installProgram(ctx, archive, add); err != nil {
			return fmt.Errorf("stable-diffusion.cpp could not be installed: %w", err)
		}
	}
	// Switching builds installs only the program.
	if m.ID == "" {
		return nil
	}
	if !s.installed(m.ID) {
		s.setStage(j, "model")
		dir := s.modelDir(m.ID)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		for _, f := range m.Files {
			url := f.URL()
			if s.fileURL != nil {
				url = s.fileURL(f)
			}
			token := ""
			if f.Gated {
				token = s.token()
			}
			if err := downloadAs(ctx, s.client(), url, token, filepath.Join(dir, filepath.Base(f.Path)), f.Size, f.SHA256, add); err != nil {
				if errors.Is(err, errNoAccess) {
					return fmt.Errorf("the download of %s was refused by Hugging Face: accept its license at %s with the account your token belongs to, then set it up again", m.Name, m.LicenseURL)
				}
				return fmt.Errorf("%s could not be downloaded: %w", f.Path, err)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, "ready"), []byte(m.ID+"\n"), 0o644); err != nil {
			return err
		}
	}
	// An editor is used for edits beside the picture model, not in its place.
	if m.EditOnly {
		return nil
	}
	return os.WriteFile(filepath.Join(s.ModelsDir, "active"), []byte(m.ID+"\n"), 0o644)
}

func (s *Setup) installProgram(ctx context.Context, archive Archive, add func(int64)) error {
	programMu.Lock()
	defer programMu.Unlock()
	if s.CLI() != "" && s.InstalledBuild() == archive.Build {
		// The other setup installed it meanwhile.
		add(archive.Size)
		return nil
	}
	dir := filepath.Join(s.ProgramDir, Release)
	if err := os.MkdirAll(s.ProgramDir, 0o755); err != nil {
		return err
	}
	zipPath := filepath.Join(s.ProgramDir, archive.Name)
	url := archive.URL()
	if (s.archive != nil || s.builds != nil) && s.fileURL != nil {
		url = s.fileURL(File{Path: archive.Name})
	}
	if err := download(ctx, s.client(), url, zipPath, archive.Size, archive.SHA256, add); err != nil {
		return err
	}
	_ = os.RemoveAll(dir)
	cli, err := extractZip(zipPath, dir)
	if err != nil {
		return err
	}
	_ = os.Remove(zipPath)
	rel, err := filepath.Rel(dir, cli)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "build"), []byte(archive.Build+"\n"), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "cli"), []byte(rel+"\n"), 0o644)
}

// UseBuild switches to the CPU build (cpu) or back to the GPU build (gpu),
// and installs it when it isn't the one installed. A setup in progress
// keeps going; it can be switched after.
func (s *Setup) UseBuild(build string) error {
	if why := s.unsupported(); why != "" {
		return errors.New(why)
	}
	prefer := ""
	switch build {
	case BuildCPU:
		prefer = BuildCPU
	case "gpu":
		if s.gpuBuildHere() == "" {
			return errors.New("this computer has no GPU the GPU build can use")
		}
	default:
		return fmt.Errorf("unknown build %q", build)
	}
	if err := os.MkdirAll(s.ProgramDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(s.ProgramDir, "prefer"), []byte(prefer+"\n"), 0o644); err != nil {
		return err
	}
	archive, ok := s.platform()
	if !ok || s.CLI() == "" || s.InstalledBuild() == archive.Build {
		if s.Changed != nil {
			s.Changed()
		}
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.job != nil && s.job.running {
		return errors.New("image generation is being set up; switch builds when it's done")
	}
	j := &job{running: true, total: archive.Size}
	ctx, cancel := context.WithCancel(context.Background())
	s.job, s.cancel = j, cancel
	go s.run(ctx, j, Model{}, true, archive)
	return nil
}

// fallBackToCPU installs the CPU build after the GPU build failed to start
// here, and keeps it: the GPU build doesn't come back on its own (#154).
func (s *Setup) fallBackToCPU(ctx context.Context) error {
	if err := os.WriteFile(filepath.Join(s.ProgramDir, "prefer"), []byte(BuildCPU+"\n"), 0o644); err != nil {
		return err
	}
	archive, ok := s.platform()
	if !ok || archive.Build != BuildCPU {
		return errors.New("there is no CPU build for this computer")
	}
	err := s.installProgram(ctx, archive, func(int64) {})
	if s.Changed != nil {
		s.Changed()
	}
	return err
}

// Cancel stops the setup in progress; downloaded parts are kept.
func (s *Setup) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
}

// Remove deletes an installed model to free its space.
func (s *Setup) Remove(id string) error {
	if _, ok := s.lookup(id); !ok {
		return fmt.Errorf("unknown model %q", id)
	}
	s.mu.Lock()
	busy := s.job != nil && s.job.running && s.job.model == id
	s.mu.Unlock()
	if busy {
		return errors.New("this model is being set up; stop the setup first")
	}
	err := os.RemoveAll(s.modelDir(id))
	if s.Changed != nil {
		s.Changed()
	}
	return err
}

func (s *Setup) token() string {
	if s.Token == nil {
		return ""
	}
	return strings.TrimSpace(s.Token())
}

// licenseFile records that the person confirmed their license covers a
// model, and when.
func (s *Setup) licenseFile(id string) string {
	return filepath.Join(s.modelDir(id), "license-accepted")
}

func (s *Setup) licenseAccepted(id string) bool {
	_, err := os.Stat(s.licenseFile(id))
	return err == nil
}

// AcceptLicense records the person's confirmation that their license for a
// model behind one covers how they'll use it (#551). Toskar can't check a
// license; it keeps the person's word, with the time and the license.
func (s *Setup) AcceptLicense(id string, now time.Time) error {
	m, ok := s.lookup(id)
	if !ok {
		return fmt.Errorf("unknown model %q", id)
	}
	if !m.NeedsLicense {
		return nil
	}
	if err := os.MkdirAll(s.modelDir(id), 0o755); err != nil {
		return err
	}
	record := fmt.Sprintf("%s\n%s\n", now.UTC().Format(time.RFC3339), m.LicenseURL)
	return os.WriteFile(s.licenseFile(id), []byte(record), 0o644)
}

// graphics is this computer's own graphics memory, or 0.
func (s *Setup) graphics() int64 {
	if s.Graphics == nil {
		return 0
	}
	return s.Graphics()
}

func (s *Setup) shared() bool {
	return s.Shared != nil && s.Shared()
}

// AllowTight records that the person agreed to set up a model that would
// be slow or near the limit here (#550).
func (s *Setup) AllowTight(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tightOK == nil {
		s.tightOK = map[string]bool{}
	}
	s.tightOK[id] = true
}

func (s *Setup) tightAllowed(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tightOK[id]
}

// tightReason says why a model would be slow or near the limit here, with
// what it needs and what this computer has (#550).
func tightReason(m Model, memory, graphics int64, shared bool) string {
	switch short, need, have := shortOf(m, memory, graphics, shared); short {
	case ShortGraphics:
		return fmt.Sprintf("%s needs about %.0f GB of graphics memory, and this computer's graphics card has %.0f GB, so parts of it run from system memory and it's slower; agree to set it up anyway, or choose a lighter model", m.Name, gb(need), gb(have))
	case ShortShared:
		return fmt.Sprintf("%s needs about %.0f GB of graphics memory, and this computer's graphics can use %.0f GB of its %.0f GB of memory, so it may be slow or fail; agree to set it up anyway, or choose a lighter model", m.Name, gb(need), gb(have), gb(memory))
	default:
		return fmt.Sprintf("%s needs about %.0f GB of memory to run well, and this computer has %.0f GB, so it may be slow or fail; agree to set it up anyway, or choose a lighter model", m.Name, gb(need), gb(have))
	}
}
