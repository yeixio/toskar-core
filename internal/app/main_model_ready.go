package app

import (
	"context"
	"sync"
	"time"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/profiles"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// settingMainReady is how the main chat model stays ready (#1472): "" until
// someone chooses, "always" (kept running, as on the Models page), or
// "managed" (Toskar keeps it ready while it fits). Until someone chooses it
// behaves as managed, and chat asks an Admin to choose.
const settingMainReady = "main_model_ready"

// settingMainReadyPinned is the model "always" kept running, so choosing
// managed again lets that one go.
const settingMainReadyPinned = "main_model_ready_pinned"

// mainReadyShare is the most of this computer's memory the main chat model
// may need and still be kept ready by Toskar. A bigger one is unloaded when
// idle, as other models are, so it doesn't hold memory others need.
const mainReadyShare = 0.5

// mainModelFor is how long the main chat model's choice is reused, since the
// idle sweep asks every 30 seconds for each loaded model.
const mainModelFor = time.Minute

type mainModelCache struct {
	mu sync.Mutex
	id string
	at time.Time
}

// mainReadyChoice is the stored choice: "", "always", or "managed".
func (a *App) mainReadyChoice(ctx context.Context) string {
	if a.Settings == nil {
		return ""
	}
	v, _ := a.Settings.GetString(ctx, settingMainReady, "")
	if v != "always" && v != "managed" {
		return ""
	}
	return v
}

// mainChatModelID is the model a new chat on the default profile answers a
// general question with, as a warm-up picks it, when it's installed here.
func (a *App) mainChatModelID(ctx context.Context) string {
	c := &a.mainModel
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.at.IsZero() && time.Since(c.at) < mainModelFor {
		return c.id
	}
	id, err := a.warmModelID(ctx, contracts.ModelWarmRequest{})
	if err != nil {
		return c.id
	}
	if id != "" && a.Models != nil {
		if m, err := a.Models.Get(ctx, id); err != nil || !m.Installed {
			id = ""
		}
	}
	c.id, c.at = id, time.Now()
	return id
}

// mainFits reports a model that needs need bytes small enough, beside total,
// for Toskar to keep ready.
func mainFits(need, total uint64) bool {
	return need > 0 && total > 0 && float64(need) <= float64(total)*mainReadyShare
}

// keptAsMain reports that the idle sweep leaves modelID loaded because it's
// the main chat model and fits, unless someone chose "always" (which keeps
// it running another way).
func (a *App) keptAsMain(ctx context.Context, modelID string) bool {
	if a.mainReadyChoice(ctx) == "always" || a.Models == nil {
		return false
	}
	if modelID == "" || a.mainChatModelID(ctx) != modelID {
		return false
	}
	m, err := a.Models.Get(ctx, modelID)
	return err == nil && mainFits(m.MemoryNeeded, a.memoryTotal(ctx))
}

// mainModelView describes the main chat model for the choice chat offers an
// Admin, or nil when there's none on this computer.
func (a *App) mainModelView(ctx context.Context) *contracts.MainModelView {
	if a.Models == nil || !auth.PrincipalFrom(ctx).Person.Role.AtLeast(auth.RoleAdmin) {
		return nil
	}
	id := a.mainChatModelID(ctx)
	if id == "" {
		return nil
	}
	m, err := a.Models.Get(ctx, id)
	if err != nil {
		return nil
	}
	total := a.memoryTotal(ctx)
	name := m.DisplayName
	if name == "" {
		name = m.ID
	}
	view := &contracts.MainModelView{
		ModelID: m.ID, Name: name, MemoryBytes: m.MemoryNeeded, MemoryTotalBytes: total,
		Fits: mainFits(m.MemoryNeeded, total), AlwaysRunning: a.alwaysRunning(ctx, m.ID),
	}
	if a.Profiles != nil {
		if p, err := a.Profiles.Get(ctx, a.defaultProfileID(ctx)); err == nil {
			view.ProfileID, view.ProfileName = p.ID, p.Name
			view.Chosen = profiles.RoleModel(p, profiles.RoleFast) == m.ID
		}
	}
	return view
}

// setMainChatModel makes modelID the main chat model: the Fast model of the
// profile new chats use, which answers quick questions on Auto (#1481).
// "" gives the choice back to Auto. When the main chat model is kept
// running, the new one is kept running instead.
func (a *App) setMainChatModel(ctx context.Context, modelID string) error {
	if a.Profiles == nil || a.Models == nil {
		return nil
	}
	if modelID != "" {
		m, err := a.Models.Get(ctx, modelID)
		if err != nil || !m.Installed || contracts.SupportRoleOf(m) != "" {
			return contracts.Errorf("INVALID_SETTING", map[string]any{"setting": "main_chat_model", "value": modelID}, "%s isn't a chat model installed on this computer", modelID)
		}
	}
	p, err := a.Profiles.Get(ctx, a.defaultProfileID(ctx))
	if err != nil {
		return err
	}
	roles := make([]contracts.ModelRole, 0, len(p.Roles)+1)
	found := false
	for _, r := range p.Roles {
		if r.Role == profiles.RoleFast {
			found = true
			if modelID == "" && r.NodeID == "" && !r.Required {
				continue
			}
			r.ModelID = modelID
		}
		roles = append(roles, r)
	}
	if !found && modelID != "" {
		roles = append(roles, contracts.ModelRole{Role: profiles.RoleFast, ModelID: modelID})
	}
	p.Roles = roles
	if err := a.Profiles.Update(ctx, p); err != nil {
		return err
	}
	a.mainModel.mu.Lock()
	a.mainModel.at = time.Time{}
	a.mainModel.mu.Unlock()
	switch a.mainReadyChoice(ctx) {
	case "always":
		return a.setMainReady(ctx, "always")
	default:
		go a.readyMainModel(context.WithoutCancel(ctx))
	}
	return nil
}

// setMainReady saves a choice. "always" keeps the main chat model running,
// which loads it now; "managed" lets go of the model "always" kept.
func (a *App) setMainReady(ctx context.Context, choice string) error {
	if choice != "always" && choice != "managed" {
		return contracts.Errorf("INVALID_SETTING", map[string]any{"setting": settingMainReady, "value": choice}, "main_model_ready must be always or managed")
	}
	pinned, _ := a.Settings.GetString(ctx, settingMainReadyPinned, "")
	switch choice {
	case "always":
		if id := a.mainChatModelID(ctx); id != "" {
			if _, err := a.setAlwaysRunning(ctx, id, true); err != nil {
				return err
			}
			if pinned != "" && pinned != id {
				_, _ = a.setAlwaysRunning(ctx, pinned, false)
			}
			pinned = id
		}
	case "managed":
		if pinned != "" {
			_, _ = a.setAlwaysRunning(ctx, pinned, false)
			pinned = ""
		}
		go a.readyMainModel(context.WithoutCancel(ctx))
	}
	if err := a.Settings.Set(ctx, settingMainReadyPinned, pinned); err != nil {
		return err
	}
	return a.Settings.Set(ctx, settingMainReady, choice)
}

// readyMainModel loads the main chat model when Toskar keeps it ready and it
// isn't loaded, so the first question after Toskar starts doesn't wait for
// it. Like a warm-up, it never loads beside a model that is answering or
// one it wouldn't fit beside.
func (a *App) readyMainModel(ctx context.Context) {
	if a.Models == nil || a.mainReadyChoice(ctx) == "always" {
		return
	}
	id := a.mainChatModelID(ctx)
	if id == "" || !a.keptAsMain(ctx, id) {
		return
	}
	running, _ := a.localRunningViews(ctx)
	_, inflight := a.warming.Load(id)
	generating := a.Health != nil && len(a.Health.Generating()) > 0
	if warmStatus(id, a.installedModels(ctx), running, inflight, generating, a.memoryTotal(ctx)) != "load" {
		return
	}
	if _, loading := a.warming.LoadOrStore(id, struct{}{}); loading {
		return
	}
	a.warmWait.Add(1)
	defer a.warmWait.Done()
	defer a.warming.Delete(id)
	load, cancel := context.WithTimeout(ctx, warmFor)
	defer cancel()
	if _, err := a.startModelLocal(load, id); err != nil && a.Logger != nil {
		a.Logger.Warn("main chat model didn't load", "model", id, "error", err)
	}
}
