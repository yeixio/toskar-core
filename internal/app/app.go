package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/yeixio/yggdrasil-core/internal/api"
	"github.com/yeixio/yggdrasil-core/internal/api/openai"
	"github.com/yeixio/yggdrasil-core/internal/artifacts"
	"github.com/yeixio/yggdrasil-core/internal/auth"
	"github.com/yeixio/yggdrasil-core/internal/automations"
	"github.com/yeixio/yggdrasil-core/internal/benchmark"
	"github.com/yeixio/yggdrasil-core/internal/config"
	"github.com/yeixio/yggdrasil-core/internal/connectors"
	"github.com/yeixio/yggdrasil-core/internal/diagnostics"
	"github.com/yeixio/yggdrasil-core/internal/discovery"
	"github.com/yeixio/yggdrasil-core/internal/events"
	"github.com/yeixio/yggdrasil-core/internal/gjallarhorn"
	"github.com/yeixio/yggdrasil-core/internal/hardware"
	"github.com/yeixio/yggdrasil-core/internal/logs"
	"github.com/yeixio/yggdrasil-core/internal/mimir"
	"github.com/yeixio/yggdrasil-core/internal/models"
	modelhealth "github.com/yeixio/yggdrasil-core/internal/models/health"
	"github.com/yeixio/yggdrasil-core/internal/models/hfclient"
	"github.com/yeixio/yggdrasil-core/internal/models/lifecycle"
	"github.com/yeixio/yggdrasil-core/internal/muninn"
	"github.com/yeixio/yggdrasil-core/internal/nodes"
	"github.com/yeixio/yggdrasil-core/internal/orchestrator"
	"github.com/yeixio/yggdrasil-core/internal/orchestrator/builtin/simple"
	"github.com/yeixio/yggdrasil-core/internal/orchestrator/builtin/team"
	"github.com/yeixio/yggdrasil-core/internal/profiles"
	"github.com/yeixio/yggdrasil-core/internal/runtimes"
	"github.com/yeixio/yggdrasil-core/internal/runtimes/external"
	"github.com/yeixio/yggdrasil-core/internal/runtimes/llamacpp"
	"github.com/yeixio/yggdrasil-core/internal/scheduler"
	"github.com/yeixio/yggdrasil-core/internal/share"
	"github.com/yeixio/yggdrasil-core/internal/store"
	"github.com/yeixio/yggdrasil-core/internal/store/repositories"
	"github.com/yeixio/yggdrasil-core/internal/tasks"
	"github.com/yeixio/yggdrasil-core/internal/tools"
	"github.com/yeixio/yggdrasil-core/internal/training"
	"github.com/yeixio/yggdrasil-core/internal/version"
	"github.com/yeixio/yggdrasil-core/pkg/contracts"
	"github.com/yeixio/yggdrasil-core/pkg/pluginapi"
)

// App is the Yggdrasil control-plane application.
type App struct {
	Config *config.Manager
	DB     *store.DB
	Bus    *events.Bus
	API    *api.Server
	Logger *slog.Logger

	Conversations *repositories.ConversationRepo
	Settings      *repositories.SettingsRepo
	Metrics       *repositories.MetricsRepo

	Models           *models.Manager
	Runtimes         *runtimes.Manager
	Profiles         *profiles.Manager
	OrchRegistry     *orchestrator.Registry
	Tasks            *tasks.Manager
	Automations      *repositories.AutomationRepo
	AutomationRunner *automations.Runner
	Scheduler        *scheduler.Scheduler
	Tools            *tools.Registry
	Nodes            *nodes.Manager
	APIKeys          *auth.APIKeyManager
	Pairing          *auth.PairingManager
	Identity         *auth.NodeIdentity
	Benchmarks       *benchmark.Runner
	HF               *hfclient.Client
	Lifecycle        *lifecycle.Sweeper
	Health           *modelhealth.Monitor
	Mimir            *mimir.Store
	Muninn           *muninn.Store
	// Notifications is Gjallarhorn's notification center and delivery.
	Notifications *gjallarhorn.Hub
	// Share admits chat, automations, benchmarks, and training by priority (§60).
	Share *share.Gate
	// Connectors are the connected services, such as GitHub (§32).
	Connectors *connectors.Manager
	// Artifacts holds chat attachments and files the assistant produced.
	Artifacts  *artifacts.Store
	summarizer *muninn.Summarizer
	// memTotal caches this computer's memory for Auto model choice.
	memTotal atomic.Uint64
	// failedModels maps a model id to when it last could not answer.
	failedModels sync.Map
	// runs maps a conversation id to its running turn, so Stop can cancel it.
	runs     sync.Map
	Training *training.Service

	hw         *hardware.Detector
	advertiser *discovery.Advertiser
	internal   *nodes.InternalServer

	stubInference bool

	// bifrostBoundLoopback is true when the Bifrost listener started on loopback;
	// enabling discovery later requires a process restart to rebind to the LAN.
	bifrostBoundLoopback  bool
	discoveryNeedsRestart bool

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// Options configures application startup.
type Options struct {
	DataDir string
	WebRoot fs.FS
	Logger  *slog.Logger
}

// New constructs and wires the application.
func New(opts Options) (*App, error) {
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}

	cfgMgr, err := config.NewManager(opts.DataDir)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	if err := cfgMgr.Update(func(c *config.Config) {
		config.ApplyEnvOverrides(c)
	}); err != nil {
		return nil, fmt.Errorf("apply env config: %w", err)
	}
	cfg := cfgMgr.Get()

	if cfg.NodeID == "" {
		nodeID := uuid.NewString()
		if err := cfgMgr.Update(func(c *config.Config) { c.NodeID = nodeID }); err != nil {
			return nil, fmt.Errorf("assign node id: %w", err)
		}
		cfg = cfgMgr.Get()
	}

	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}

	bus := events.NewBus(128)
	convRepo := repositories.NewConversationRepo(db.SQL)
	settingsRepo := repositories.NewSettingsRepo(db.SQL)
	metricsRepo := repositories.NewMetricsRepo(db.SQL)

	secrets := auth.NewSecretStore(cfg.DataDir)
	identity, err := auth.LoadOrCreateIdentity(secrets, cfg.NodeID)
	if err != nil {
		return nil, fmt.Errorf("identity: %w", err)
	}

	catalog, err := models.NewCatalog(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	modelStorage := models.NewStorage(db.SQL, cfg.ModelsDir)
	downloader := models.NewDownloader(modelStorage, bus, db.SQL)
	downloader.QuotaLimitBytes = func(ctx context.Context) (uint64, error) {
		gb, err := settingsRepo.GetInt(ctx, "model_storage_limit_gb", 0)
		if err != nil || gb <= 0 {
			return 0, nil
		}
		return uint64(gb) * 1024 * 1024 * 1024, nil
	}
	modelMgr := models.NewManager(catalog, modelStorage, downloader, bus)
	if presets, err := models.LoadPresets(cfg.DataDir); err == nil {
		modelMgr.SetPresets(presets)
	}
	modelMgr.SyncDynamic(context.Background())

	llamaClient := llamacpp.NewClient()
	rtRegistry := runtimes.NewRegistry()
	llamaRT := llamacpp.New(cfg.RuntimesDir, cfg.LogsDir)
	healthMonitor := modelhealth.NewMonitor(modelhealth.DefaultSettings())
	healthMonitor.Log = logger
	healthMonitor.Probe = llamacpp.Probe
	healthMonitor.Publish = func(evt modelhealth.Event) {
		bus.Publish(events.New(evt.Type, evt.Payload))
	}
	rtRegistry.Register(llamaRT)
	rtRegistry.Register(external.New(external.Config{}))
	rtMgr := runtimes.NewManager(rtRegistry, db.SQL, bus, llamaClient)

	profileMgr := profiles.NewManager(db.SQL)
	if err := profileMgr.EnsurePresets(context.Background()); err != nil {
		return nil, fmt.Errorf("profiles: %w", err)
	}

	orchReg := orchestrator.NewRegistry()
	orchReg.Register(simple.New())
	orchReg.Register(team.New())

	sched := scheduler.New(bus)
	wd, _ := os.Getwd()
	toolReg := tools.NewRegistry(wd, bus)

	pairing := auth.NewPairingManager(db.SQL, identity)
	apiKeyMgr := auth.NewAPIKeyManager(db.SQL, secrets)

	a := &App{
		Share:         share.New(0),
		Config:        cfgMgr,
		DB:            db,
		Bus:           bus,
		Logger:        logger,
		Conversations: convRepo,
		Settings:      settingsRepo,
		Metrics:       metricsRepo,
		Models:        modelMgr,
		Runtimes:      rtMgr,
		Profiles:      profileMgr,
		OrchRegistry:  orchReg,
		Scheduler:     sched,
		Tools:         toolReg,
		APIKeys:       apiKeyMgr,
		Pairing:       pairing,
		Identity:      identity,
		hw:            &hardware.Detector{DataPath: cfg.DataDir},
		stubInference: config.EnvTruthy("YGGDRASIL_STUB_INFERENCE"),
		Health:        healthMonitor,
	}
	healthMonitor.Stopper = llamaStopper{rt: rtMgr}
	healthMonitor.Memory = a.memoryPressure
	llamaRT.OnProcessExit(func(instanceID, modelID string, exitCode int, stderrTail string) {
		a.Health.ReportProcessExit(instanceID, exitCode, stderrTail)
		a.Logger.Info("model runtime exited", "model_id", modelID, "running_model_id", instanceID, "exit_code", exitCode)
	})

	if a.stubInference {
		if err := modelMgr.EnsureStubModel(context.Background()); err != nil {
			return nil, fmt.Errorf("stub model: %w", err)
		}
		logger.Info("stub inference enabled", "model_id", models.StubModelID)
	}
	a.loadDisabledTools(context.Background())
	// Connected services add tools; their credentials stay in the secrets
	// directory and are added only when a tool runs (§32).
	a.Connectors = connectors.NewManager(db.SQL, secrets, toolReg, connectors.GitHub{}, connectors.HomeAssistant{})
	if err := a.Connectors.Load(context.Background()); err != nil {
		logger.Warn("load connected services", "error", err)
	}

	bench := benchmark.NewRunner()
	bench.ModelPath = modelMgr.Path
	bench.StartModel = func(ctx context.Context, modelID, modelPath string) (pluginapi.RunningModel, error) {
		return rtMgr.StartModel(ctx, "llamacpp", pluginapi.ModelStartConfig{
			ModelID: modelID, ModelPath: modelPath, Adapters: a.localAdapters(ctx, modelID),
		})
	}
	bench.Admit = func(ctx context.Context, waiting func(string)) (func(), error) {
		work, err := a.enterWork(ctx, share.Benchmark, "benchmark", waiting)
		return work.Done, err
	}
	bench.StopModel = func(ctx context.Context, instanceID string) error {
		return rtMgr.StopModel(ctx, "llamacpp", instanceID)
	}
	bench.ListRunning = func(ctx context.Context) ([]pluginapi.RunningModel, error) {
		return rtMgr.ListRunning(ctx, "llamacpp")
	}
	bench.Chat = rtMgr.Chat
	a.Benchmarks = bench

	a.Nodes = nodes.NewManager(db.SQL, bus, pairing, cfg.NodeID, cfg.NodeName, a.detectHardware)
	a.Nodes.SetAdvertiseAddr(a.bifrostAdvertiseAddr)
	a.Nodes.SetStaticPeers(cfg.StaticPeers)
	_ = a.syncInternalBind()

	a.Tasks = tasks.NewManager(db.SQL, bus, profileMgr, orchReg, rtMgr, sched, toolReg, a.listNodes, modelMgr.Path)
	a.Tasks.SetClusterHooks(a.placeRole, func(ctx context.Context, nodeID, modelID string, messages []pluginapi.ChatMessage) (<-chan pluginapi.ChatChunk, error) {
		return a.generateOnNode(ctx, nodeID, modelID, "", "", messages)
	})

	openaiHandler := &openai.Handler{
		Profiles: profileMgr,
		Runtimes: rtMgr,
		Chat:     a,
		Bus:      bus,
		Auth:     a.openAIAuth,
	}

	webRoot := opts.WebRoot
	if webRoot == nil {
		if root, err := api.ResolveWebRoot(cfg.WebUIDir); err == nil {
			webRoot = root
		}
	}

	a.API = api.NewServer(api.Dependencies{
		Config:   cfgMgr,
		Bus:      bus,
		Logger:   logger,
		WebRoot:  webRoot,
		OpenAI:   openaiHandler,
		Hardware: a.detectHardware,
		ListConversations: func(ctx context.Context) ([]contracts.Conversation, error) {
			return convRepo.List(ctx)
		},
		CreateConversation: func(ctx context.Context, title, profileID, modelID string) (contracts.Conversation, error) {
			return convRepo.Create(ctx, title, profileID, modelID)
		},
		UpdateConversation: func(ctx context.Context, id string, title, profileID, modelID *string, memoryOff *bool) (contracts.Conversation, error) {
			return convRepo.Update(ctx, id, repositories.ConversationPatch{
				Title:     title,
				ProfileID: profileID,
				ModelID:   modelID,
				MemoryOff: memoryOff,
			})
		},
		DeleteConversation: func(ctx context.Context, id string) error {
			if a.Artifacts != nil {
				if err := a.Artifacts.DeleteConversation(ctx, id); err != nil {
					return err
				}
			}
			return convRepo.Delete(ctx, id)
		},
		ListMessages: func(ctx context.Context, id string) ([]contracts.Message, error) {
			return convRepo.ListMessages(ctx, id)
		},
		GetSettings: func(ctx context.Context) (contracts.SettingsView, error) {
			return a.settingsView(ctx)
		},
		UpdateSettings: func(ctx context.Context, patch map[string]any) (contracts.SettingsView, error) {
			if err := a.applySettingsPatch(ctx, patch); err != nil {
				return contracts.SettingsView{}, err
			}
			return a.settingsView(ctx)
		},
		ResetApp: a.ResetApp,
		ListPerformance: func(ctx context.Context, sort, order string, limit int) ([]contracts.GenerationRun, error) {
			if a.Metrics == nil {
				return []contracts.GenerationRun{}, nil
			}
			return a.Metrics.List(ctx, repositories.ListFilter{Sort: sort, Order: order, Limit: limit})
		},
		ListBenchmarkWorkloads: func() []contracts.BenchmarkWorkload {
			return a.Benchmarks.ListWorkloads()
		},
		StartBenchmark: func(ctx context.Context, req contracts.BenchmarkRequest) (*contracts.BenchmarkJob, error) {
			// Detach from the HTTP request context so the job survives after 202.
			return a.Benchmarks.Start(context.Background(), req)
		},
		GetBenchmark: func(ctx context.Context, id string) (*contracts.BenchmarkJob, error) {
			return a.Benchmarks.Get(id)
		},
		ListBenchmarks: func(ctx context.Context) ([]contracts.BenchmarkJob, error) {
			return a.Benchmarks.List(), nil
		},
		CancelBenchmark: func(ctx context.Context, id string) error {
			return a.Benchmarks.Cancel(id)
		},
		ExportDiagnostics: func(ctx context.Context, includeConversations bool) (string, error) {
			return a.exportDiagnostics(ctx, includeConversations)
		},
		ListLogs: func(ctx context.Context) ([]logs.Entry, error) {
			return (&logs.Store{Dir: a.Config.Get().LogsDir}).List()
		},
		GetLog: func(ctx context.Context, name string, tailBytes int64) (logs.Content, error) {
			return (&logs.Store{Dir: a.Config.Get().LogsDir}).ReadTail(name, tailBytes)
		},
		Version: func() contracts.VersionResponse {
			offer := version.CurrentOffer()
			return contracts.VersionResponse{
				Version: offer.Version, Commit: offer.Commit,
				BuildDate: version.BuildDate, Product: "Yggdrasil",
				License: offer.License, Source: offer.Source,
			}
		},
		ListModels: a.listModelsCluster,
		RecommendModels: func(ctx context.Context, purpose string) (contracts.Recommendation, error) {
			hw, _ := a.detectHardware(ctx)
			return modelMgr.Recommend(ctx, purpose, hw)
		},
		ModelsFit: a.modelsFitAll,
		BrowseModels: func(ctx context.Context, query string, limit int) ([]contracts.BrowseModel, error) {
			if a.HF == nil {
				a.HF = hfclient.New()
			}
			return a.HF.Search(ctx, query, limit)
		},
		InstallModel: func(ctx context.Context, id string, wait bool, nodeID string) error {
			return a.installModelOn(ctx, id, nodeID, wait)
		},
		InstallModelFromURL: func(ctx context.Context, req contracts.InstallFromURLRequest, wait bool) (string, error) {
			return a.installFromURLOn(ctx, req, wait)
		},
		DeleteModel: func(ctx context.Context, id, nodeID string) error {
			return a.deleteModelOn(ctx, id, nodeID)
		},
		ListRunningModels: a.listRunningAll,
		StartModel: func(ctx context.Context, id, nodeID string) (contracts.RunningModelView, error) {
			return a.startModel(ctx, id, nodeID)
		},
		StopModel: func(ctx context.Context, instanceID, nodeID string) error {
			return a.stopModel(ctx, instanceID, nodeID)
		},
		ListProfiles: profileMgr.List,
		CreateProfile: func(ctx context.Context, p contracts.AIProfile) (contracts.AIProfile, error) {
			return profileMgr.Create(ctx, p)
		},
		GetProfile: profileMgr.Get,
		UpdateProfile: func(ctx context.Context, p contracts.AIProfile) (contracts.AIProfile, error) {
			if err := profileMgr.Update(ctx, p); err != nil {
				return contracts.AIProfile{}, err
			}
			return profileMgr.Get(ctx, p.ID)
		},
		DeleteProfile: profileMgr.Delete,
		ListRuntimes:  rtMgr.List,
		InstallRuntime: func(ctx context.Context, id string) error {
			return rtMgr.Install(ctx, id, runtimes.InstallOptions{})
		},
		ListNodes: func(ctx context.Context) ([]contracts.Node, error) {
			return a.listNodesWithHardware(ctx)
		},
		RefreshDiscovery:      a.Nodes.RefreshDiscovery,
		StartPairing:          a.Nodes.StartPairing,
		ClaimPairing:          a.Nodes.ClaimPairing,
		ApprovePairing:        a.Nodes.ApprovePairing,
		ListPairingOffers:     a.Nodes.ListIncomingOffers,
		ReceivePairingOffer:   a.Nodes.ReceiveOffer,
		LookupOutboundPairing: a.Nodes.LookupOutbound,
		AdvertiseAddr:         a.bifrostAdvertiseAddr,
		LocalCertPEM: func() string {
			if a.Identity == nil {
				return ""
			}
			return string(a.Identity.CertPEM)
		},
		RevokeNode: a.Nodes.Revoke,
		ListTasks:  a.Tasks.List,
		CreateTask: a.Tasks.Create,
		GetTask:    a.Tasks.Get,
		RunTask:    a.Tasks.Run,
		DecideTool: toolReg.Decide,
		ListTools: func(ctx context.Context) (any, error) {
			return a.listToolViews(ctx)
		},
		SetToolEnabled: a.setToolEnabled,
		TestTool:       a.testTool,
		ToolActivity: func() any {
			return toolReg.Recent()
		},
		ListAPIKeys:  apiKeyMgr.List,
		CreateAPIKey: apiKeyMgr.Create,
		RevokeAPIKey: apiKeyMgr.Revoke,
		RotateAPIKey: apiKeyMgr.Rotate,
		VerifyAPIKey: apiKeyMgr.Verify,
		StopChat:     a.StopChat,
		Chat: func(w http.ResponseWriter, r *http.Request, conversationID, profileID, modelID, message string, stream bool, execution string) error {
			return a.HandleHTTPChat(w, r, conversationID, profileID, modelID, message, stream, execution)
		},
	})

	a.HF = hfclient.New()
	a.Lifecycle = &lifecycle.Sweeper{
		Logger: logger,
		OnUnload: func(inst lifecycle.Instance) {
			a.Bus.Publish(events.New(events.ModelUnloaded, map[string]any{
				"model_id":    inst.ModelID,
				"instance_id": inst.InstanceID,
				"reason":      "idle",
			}))
		},
		List: func(ctx context.Context) ([]lifecycle.Instance, error) {
			views, err := a.localRunningViews(ctx)
			if err != nil {
				return nil, err
			}
			out := make([]lifecycle.Instance, 0, len(views))
			for _, v := range views {
				inst := lifecycle.Instance{InstanceID: v.InstanceID, ModelID: v.ModelID}
				if v.LastUsedAt != nil {
					inst.LastUsed = *v.LastUsedAt
				} else if t, ok := runtimeLastUsed.Load(v.ModelID); ok {
					inst.LastUsed = t.(time.Time)
				}
				out = append(out, inst)
			}
			return out, nil
		},
		Stop: func(ctx context.Context, instanceID string) error {
			return a.stopModelLocal(ctx, instanceID)
		},
		Settings: func(ctx context.Context) (string, int, error) {
			mode, _ := a.Settings.GetString(ctx, "model_lifecycle", "automatic")
			mins, _ := a.Settings.GetInt(ctx, "idle_unload_minutes", 15)
			return mode, mins, nil
		},
	}

	// Gjallarhorn: every notice is kept in the notification center; the
	// desktop is one delivery channel.
	a.Notifications = gjallarhorn.NewHub(db.SQL, bus, desktopChannel{settings: settingsRepo, send: automations.OSSender{}})
	a.API.BindNotifications(a.Notifications)
	a.API.BindConnectors(a.Connectors)

	autoRepo := repositories.NewAutomationRepo(db.SQL)
	a.Automations = autoRepo
	a.AutomationRunner = &automations.Runner{
		Store:  autoRepo,
		Exec:   automationExecutor{app: a},
		Notify: automationNotifier{settings: settingsRepo, send: automations.OSSender{}, hub: a.Notifications},
		Bus:    bus,
		Logger: logger,
		Pause: func(ctx context.Context, id string) error {
			enabled := false
			_, err := a.Automations.Update(ctx, id, automations.Patch{Enabled: &enabled}, time.Now())
			return err
		},
	}
	a.API.BindAutomations(api.Dependencies{
		ListAutomations: a.Automations.List,
		CreateAutomation: func(ctx context.Context, in automations.CreateInput) (automations.Automation, error) {
			created, err := a.Automations.Create(ctx, in, time.Now())
			if err != nil {
				return automations.Automation{}, err
			}
			if err := a.enableBackgroundWhenScheduled(ctx); err != nil && a.Logger != nil {
				a.Logger.Warn("could not keep the daemon running for schedules", "error", err)
			}
			return created, nil
		},
		GetAutomation: a.Automations.History,
		UpdateAutomation: func(ctx context.Context, id string, patch automations.Patch) (automations.Automation, error) {
			return a.Automations.Update(ctx, id, patch, time.Now())
		},
		DeleteAutomation:  a.Automations.Delete,
		RunAutomation:     a.AutomationRunner.RunNow,
		PreviewAutomation: a.AutomationRunner.Preview,
		PauseAutomation: func(ctx context.Context, id string) (automations.Automation, error) {
			enabled := false
			return a.Automations.Update(ctx, id, automations.Patch{Enabled: &enabled}, time.Now())
		},
		ResumeAutomation: func(ctx context.Context, id string) (automations.Automation, error) {
			enabled := true
			return a.Automations.Update(ctx, id, automations.Patch{Enabled: &enabled}, time.Now())
		},
	})

	a.Mimir = mimir.NewStore(db.SQL, filepath.Join(cfg.DataDir, "knowledge"))
	a.Muninn = muninn.NewStore(db.SQL)
	a.summarizer = &muninn.Summarizer{Store: a.Muninn}
	a.API.BindMemory(a.Muninn)
	a.Artifacts = artifacts.NewStore(db.SQL, filepath.Join(cfg.DataDir, "artifacts"))
	a.Tools.Register(&artifacts.CreateTool{Store: a.Artifacts})
	a.API.BindArtifacts(a.Artifacts)
	a.API.BindKnowledge(a.Mimir)
	a.Training = a.newTrainingService()
	if err := a.Training.Recover(context.Background()); err != nil {
		return nil, fmt.Errorf("training: %w", err)
	}
	a.API.BindTraining(a.Training, modelMgr.Catalog().List)
	openaiHandler.Specialized = a.Training.DeployedModels

	a.internal = nodes.NewInternalServer(nodes.InternalDeps{
		Config:          a.Config.Get(),
		Logger:          logger,
		Identity:        identity,
		Pairing:         pairing,
		Hardware:        a.detectHardware,
		ListModels:      modelMgr.List,
		InstallModel:    modelMgr.Install,
		InstallFromURL:  modelMgr.InstallFromURL,
		DeleteModel:     modelMgr.Delete,
		ListRunning:     a.localRunningViews,
		StartModel:      a.startModelLocal,
		StopModel:       a.stopModelLocal,
		Chat:            a.internalChat,
		ReceiveOffer:    a.Nodes.ReceiveOffer,
		CompletePairing: a.Nodes.CompletePairing,
		ListIncoming:    a.Nodes.ListIncomingOffers,
		LookupOutbound:  a.Nodes.LookupOutbound,
		AdvertiseAddr:   a.bifrostAdvertiseAddr,
		Training:        a.Training.RemoteHandler(),
	})

	return a, nil
}

func (a *App) detectHardware(ctx context.Context) (contracts.HardwareInventory, error) {
	return a.hw.Detect(ctx)
}

func (a *App) openAIAuth(r *http.Request) error {
	return a.authorizeControlRequest(r)
}

func (a *App) authorizeControlRequest(r *http.Request) error {
	if !config.ListensBeyondLoopback(a.Config.Get().APIHost) {
		return nil
	}
	token, err := auth.BearerToken(r)
	if err != nil {
		return err
	}
	if _, err := a.APIKeys.Verify(r.Context(), token); err != nil {
		return err
	}
	return nil
}

// requireKeyForRemoteBind refuses a non-loopback control API with no key.
// YGGDRASIL_API_KEY, when set, is hashed and stored if it is not already valid.
func (a *App) requireKeyForRemoteBind(ctx context.Context) error {
	cfg := a.Config.Get()
	if !config.ListensBeyondLoopback(cfg.APIHost) {
		return nil
	}
	if supplied := strings.TrimSpace(os.Getenv("YGGDRASIL_API_KEY")); supplied != "" {
		if _, err := a.APIKeys.Adopt(ctx, "bootstrap", supplied); err != nil {
			return fmt.Errorf("configure API key: %w", err)
		}
		return nil
	}
	keys, err := a.APIKeys.List(ctx)
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		return fmt.Errorf("%w (api host %s)", auth.ErrAPIKeyRequired, cfg.APIHost)
	}
	return nil
}

// Start runs the API server until context cancellation.
func (a *App) Start(ctx context.Context) error {
	ctx, a.cancel = context.WithCancel(ctx)
	a.notifyFromEvents(ctx)
	_ = a.syncInternalBind()
	cfg := a.Config.Get()
	if err := a.requireKeyForRemoteBind(ctx); err != nil {
		return err
	}
	if err := a.enableBackgroundWhenScheduled(ctx); err != nil && a.Logger != nil {
		a.Logger.Warn("could not keep the daemon running for schedules", "error", err)
	}
	if cfg.DiscoveryEnabled && (cfg.InternalHost == "" || cfg.InternalHost == "127.0.0.1" || cfg.InternalHost == "localhost") {
		_ = a.Config.Update(func(c *config.Config) { c.InternalHost = "0.0.0.0" })
		cfg = a.Config.Get()
	}

	a.Logger.Info("yggdrasil starting",
		"version", version.Version,
		"node_id", cfg.NodeID,
		"data_dir", cfg.DataDir,
		"api", cfg.APIAddr(),
		"bifrost", cfg.InternalAddr(),
		"advertise", a.bifrostAdvertiseAddr(),
		"discovery", cfg.DiscoveryEnabled,
	)

	host := cfg.InternalHost
	a.bifrostBoundLoopback = host == "" || host == "127.0.0.1" || host == "localhost"

	if cfg.DiscoveryEnabled || len(cfg.StaticPeers) > 0 {
		if cfg.DiscoveryEnabled {
			adv, err := discovery.StartAdvertise(cfg, true)
			if err != nil {
				a.Logger.Warn("mdns advertise failed", "error", err)
			} else {
				a.advertiser = adv
			}
		}
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			// Immediate + short retries so Docker static peers come up across container boot order.
			for i := 0; i < 15; i++ {
				_ = a.Nodes.RefreshDiscovery(ctx)
				select {
				case <-ctx.Done():
					return
				case <-time.After(2 * time.Second):
				}
			}
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					_ = a.Nodes.RefreshDiscovery(ctx)
				}
			}
		}()
	}

	// Keep paired-computer liveness fresh so chat placement can reuse it,
	// whether or not discovery is on.
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		ticker := time.NewTicker(nodes.LivenessInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.Nodes.RefreshPairedLiveness(ctx)
			}
		}
	}()

	if a.Lifecycle != nil {
		a.Lifecycle.Start(ctx)
	}
	if a.AutomationRunner != nil {
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			a.AutomationRunner.Start(ctx)
		}()
	}
	if a.Health != nil {
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			a.Health.Run(ctx)
		}()
	}

	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		addr := cfg.InternalAddr()
		a.Logger.Info("starting bifrost listener", "addr", addr)
		if err := a.internal.ListenAndServe(addr); err != nil && ctx.Err() == nil {
			a.Logger.Error("internal server failed", "error", err, "addr", addr)
		}
	}()

	errCh := make(chan error, 1)
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		if err := a.API.ListenAndServe(cfg.APIAddr()); err != nil && ctx.Err() == nil {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		return a.Shutdown(context.Background())
	case err := <-errCh:
		return err
	}
}

// Shutdown stops services and closes resources.
func (a *App) Shutdown(ctx context.Context) error {
	if a.cancel != nil {
		a.cancel()
	}
	if a.advertiser != nil {
		a.advertiser.Stop()
	}
	if a.Lifecycle != nil {
		a.Lifecycle.Halt()
	}

	// Unload models first so llama-server children exit before HTTP shutdown.
	if a.Runtimes != nil {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 3*time.Second)
		a.Runtimes.StopAll(stopCtx)
		stopCancel()
	}

	shutdownCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	_ = a.API.Shutdown(shutdownCtx)
	_ = a.internal.Shutdown(shutdownCtx)
	a.wg.Wait()
	if a.DB != nil {
		_ = a.DB.Close()
	}
	a.Logger.Info("yggdrasil stopped")
	return nil
}

func (a *App) settingsView(ctx context.Context) (contracts.SettingsView, error) {
	cfg := a.Config.Get()
	advanced, _ := a.Settings.GetBool(ctx, "advanced_mode", false)
	lifecycle, _ := a.Settings.GetString(ctx, "model_lifecycle", "automatic")
	idleMins, _ := a.Settings.GetInt(ctx, "idle_unload_minutes", 15)
	keepBackground, _ := a.Settings.GetBool(ctx, "keep_running_in_background", false)
	defaultProfile, _ := a.Settings.GetString(ctx, "default_profile_id", "")
	defaultExec, _ := a.Settings.GetString(ctx, "default_execution", "automatic")
	downloadBehavior, _ := a.Settings.GetString(ctx, "download_behavior", "ask")
	storageLimit, _ := a.Settings.GetInt(ctx, "model_storage_limit_gb", 0)
	saveChat, _ := a.Settings.GetBool(ctx, "save_chat_history", true)
	memoryEnabled, _ := a.Settings.GetBool(ctx, "memory_enabled", true)
	saveTask, _ := a.Settings.GetBool(ctx, "save_task_history", true)
	notifyTask, _ := a.Settings.GetBool(ctx, "notify_task_finish", true)
	notifyPeer, _ := a.Settings.GetBool(ctx, "notify_peer_offline", true)
	toolTerminal, _ := a.Settings.GetString(ctx, "tool_terminal", "ask")
	toolFiles, _ := a.Settings.GetString(ctx, "tool_file_writes", "ask")
	toolGit, _ := a.Settings.GetString(ctx, "tool_git", "ask")
	launchAtLogin, _ := a.Settings.GetBool(ctx, "launch_at_login", false)
	if defaultExec == "" {
		defaultExec = "automatic"
	}
	if downloadBehavior == "" {
		downloadBehavior = "ask"
	}
	return contracts.SettingsView{
		DataDir: cfg.DataDir, ModelsDir: cfg.ModelsDir, RuntimesDir: cfg.RuntimesDir,
		LogsDir: cfg.LogsDir, APIHost: cfg.APIHost, APIPort: cfg.APIPort,
		LANAPIEnabled: cfg.LANAPIEnabled, WebUIEnabled: cfg.WebUIEnabled,
		DiscoveryEnabled: cfg.DiscoveryEnabled, NodeName: cfg.NodeName, NodeID: cfg.NodeID,
		AdvancedMode: advanced, ModelLifecycle: lifecycle, IdleUnloadMinutes: idleMins,
		KeepRunningInBackground: keepBackground,
		DefaultProfileID:        defaultProfile,
		DefaultExecution:        defaultExec,
		DownloadBehavior:        downloadBehavior,
		ModelStorageLimitGB:     storageLimit,
		SaveChatHistory:         saveChat,
		MemoryEnabled:           memoryEnabled,
		SaveTaskHistory:         saveTask,
		NotifyTaskFinish:        notifyTask,
		NotifyPeerOffline:       notifyPeer,
		ToolTerminal:            toolTerminal,
		ToolFileWrites:          toolFiles,
		ToolGit:                 toolGit,
		LaunchAtLogin:           launchAtLogin,
		DiscoveryNeedsRestart:   a.discoveryNeedsRestart,
	}, nil
}

func setSettingString(ctx context.Context, repo *repositories.SettingsRepo, key, value string, allowed map[string]bool) error {
	if allowed != nil && !allowed[value] {
		return fmt.Errorf("invalid %s value %q", key, value)
	}
	return repo.Set(ctx, key, value)
}

func (a *App) applySettingsPatch(ctx context.Context, patch map[string]any) error {
	if v, ok := patch["memory_enabled"].(bool); ok {
		if err := a.Settings.SetBool(ctx, "memory_enabled", v); err != nil {
			return err
		}
	}
	if v, ok := patch["advanced_mode"].(bool); ok {
		if err := a.Settings.SetBool(ctx, "advanced_mode", v); err != nil {
			return err
		}
	}
	if v, ok := patch["keep_running_in_background"].(bool); ok {
		if err := a.Settings.SetBool(ctx, "keep_running_in_background", v); err != nil {
			return err
		}
	}
	if v, ok := patch["model_lifecycle"].(string); ok && v != "" {
		if v != "automatic" && v != "manual" {
			return fmt.Errorf("model_lifecycle must be automatic or manual")
		}
		if err := a.Settings.Set(ctx, "model_lifecycle", v); err != nil {
			return err
		}
	}
	if v, ok := patch["idle_unload_minutes"].(float64); ok {
		if err := a.Settings.SetInt(ctx, "idle_unload_minutes", int(v)); err != nil {
			return err
		}
	}
	if v, ok := patch["idle_unload_minutes"].(int); ok {
		if err := a.Settings.SetInt(ctx, "idle_unload_minutes", v); err != nil {
			return err
		}
	}
	if v, ok := patch["default_profile_id"].(string); ok {
		if err := a.Settings.Set(ctx, "default_profile_id", v); err != nil {
			return err
		}
	}
	if v, ok := patch["default_execution"].(string); ok && v != "" {
		if err := setSettingString(ctx, a.Settings, "default_execution", v, map[string]bool{
			"automatic": true, "local": true, "ask": true,
		}); err != nil {
			return err
		}
	}
	if v, ok := patch["download_behavior"].(string); ok && v != "" {
		if err := setSettingString(ctx, a.Settings, "download_behavior", v, map[string]bool{
			"ask": true, "automatic": true,
		}); err != nil {
			return err
		}
	}
	if v, ok := patch["model_storage_limit_gb"].(float64); ok {
		if err := a.Settings.SetInt(ctx, "model_storage_limit_gb", int(v)); err != nil {
			return err
		}
	}
	if v, ok := patch["model_storage_limit_gb"].(int); ok {
		if err := a.Settings.SetInt(ctx, "model_storage_limit_gb", v); err != nil {
			return err
		}
	}
	for _, key := range []string{"save_chat_history", "save_task_history", "notify_task_finish", "notify_peer_offline", "launch_at_login"} {
		if v, ok := patch[key].(bool); ok {
			if err := a.Settings.SetBool(ctx, key, v); err != nil {
				return err
			}
		}
	}
	toolAllowed := map[string]bool{"deny": true, "ask": true, "allow": true, "allow-for-session": true}
	if v, ok := patch["tool_terminal"].(string); ok && v != "" {
		if err := setSettingString(ctx, a.Settings, "tool_terminal", v, toolAllowed); err != nil {
			return err
		}
	}
	if v, ok := patch["tool_file_writes"].(string); ok && v != "" {
		if err := setSettingString(ctx, a.Settings, "tool_file_writes", v, toolAllowed); err != nil {
			return err
		}
	}
	if v, ok := patch["tool_git"].(string); ok && v != "" {
		if err := setSettingString(ctx, a.Settings, "tool_git", v, toolAllowed); err != nil {
			return err
		}
	}

	var discoveryTouched bool
	var discoveryEnabled bool
	if v, ok := patch["lan_api_enabled"].(bool); ok && v {
		keys, err := a.APIKeys.List(ctx)
		if err != nil {
			return err
		}
		if len(keys) == 0 {
			return auth.ErrAPIKeyRequired
		}
	}
	err := a.Config.Update(func(c *config.Config) {
		if v, ok := patch["node_name"].(string); ok && v != "" {
			c.NodeName = v
		}
		if v, ok := patch["lan_api_enabled"].(bool); ok {
			c.LANAPIEnabled = v
			if v {
				c.APIHost = "0.0.0.0"
			} else {
				c.APIHost = config.DefaultBindLoopback
			}
		}
		if v, ok := patch["discovery_enabled"].(bool); ok {
			discoveryTouched = true
			discoveryEnabled = v
			c.DiscoveryEnabled = v
			if v {
				c.InternalHost = "0.0.0.0"
			} else {
				c.InternalHost = config.DefaultBindLoopback
			}
		}
		if v, ok := patch["web_ui_enabled"].(bool); ok {
			c.WebUIEnabled = v
		}
	})
	if err != nil {
		return err
	}
	if discoveryTouched {
		a.reloadDiscovery()
		if discoveryEnabled && a.bifrostBoundLoopback {
			a.discoveryNeedsRestart = true
		} else if !discoveryEnabled {
			a.discoveryNeedsRestart = false
		}
	}
	return nil
}

func (a *App) reloadDiscovery() {
	if a.advertiser != nil {
		a.advertiser.Stop()
		a.advertiser = nil
	}
	_ = a.syncInternalBind()
	cfg := a.Config.Get()
	if cfg.DiscoveryEnabled {
		adv, err := discovery.StartAdvertise(cfg, true)
		if err != nil {
			a.Logger.Warn("mdns advertise failed after settings change", "error", err)
		} else {
			a.advertiser = adv
		}
	}
	if a.Nodes != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			_ = a.Nodes.RefreshDiscovery(ctx)
		}()
	}
}

// enableBackgroundWhenScheduled turns on keep_running_in_background when a schedule
// exists. The desktop shell quits the daemon on window close unless that flag is set.
func (a *App) enableBackgroundWhenScheduled(ctx context.Context) error {
	if a.Automations == nil || a.Settings == nil {
		return nil
	}
	items, err := a.Automations.List(ctx)
	if err != nil || len(items) == 0 {
		return err
	}
	on, err := a.Settings.GetBool(ctx, "keep_running_in_background", false)
	if err != nil || on {
		return err
	}
	return a.Settings.SetBool(ctx, "keep_running_in_background", true)
}

// ResetApp restores first-run defaults: settings, profiles, chats, tasks, and API keys.
// Downloaded models and runtimes are kept unless deleteModels is true.
func (a *App) ResetApp(ctx context.Context, deleteModels bool) (contracts.SettingsView, error) {
	defaults := config.DefaultConfig()
	if err := a.Settings.SetBool(ctx, "advanced_mode", false); err != nil {
		return contracts.SettingsView{}, err
	}
	_ = a.Settings.SetBool(ctx, "keep_running_in_background", false)
	_ = a.Settings.Set(ctx, "model_lifecycle", "automatic")
	_ = a.Settings.SetInt(ctx, "idle_unload_minutes", 15)
	_ = a.Settings.Set(ctx, "default_profile_id", "")
	_ = a.Settings.Set(ctx, "default_execution", "automatic")
	_ = a.Settings.Set(ctx, "download_behavior", "ask")
	_ = a.Settings.SetInt(ctx, "model_storage_limit_gb", 0)
	_ = a.Settings.SetBool(ctx, "save_chat_history", true)
	_ = a.Settings.SetBool(ctx, "save_task_history", true)
	_ = a.Settings.SetBool(ctx, "notify_task_finish", true)
	_ = a.Settings.SetBool(ctx, "notify_peer_offline", true)
	_ = a.Settings.SetBool(ctx, "launch_at_login", false)
	_ = a.Settings.Set(ctx, "tool_terminal", "ask")
	_ = a.Settings.Set(ctx, "tool_file_writes", "ask")
	_ = a.Settings.Set(ctx, "tool_git", "ask")
	a.discoveryNeedsRestart = false
	if err := a.Config.Update(func(c *config.Config) {
		c.LANAPIEnabled = defaults.LANAPIEnabled
		c.WebUIEnabled = defaults.WebUIEnabled
		c.DiscoveryEnabled = defaults.DiscoveryEnabled
		c.APIHost = defaults.APIHost
		c.InternalHost = defaults.InternalHost
		if c.DiscoveryEnabled {
			c.InternalHost = "0.0.0.0"
		}
		c.NodeName = defaults.NodeName
	}); err != nil {
		return contracts.SettingsView{}, err
	}
	a.reloadDiscovery()
	if err := a.Conversations.DeleteAll(ctx); err != nil {
		return contracts.SettingsView{}, fmt.Errorf("clear conversations: %w", err)
	}
	if a.Artifacts != nil {
		if err := a.Artifacts.DeleteAll(ctx); err != nil {
			return contracts.SettingsView{}, fmt.Errorf("clear files: %w", err)
		}
	}
	if a.Metrics != nil {
		if err := a.Metrics.DeleteAll(ctx); err != nil {
			return contracts.SettingsView{}, fmt.Errorf("clear metrics: %w", err)
		}
	}
	if a.DB != nil {
		if _, err := a.DB.SQL.ExecContext(ctx, `DELETE FROM task_steps`); err != nil {
			return contracts.SettingsView{}, fmt.Errorf("clear task steps: %w", err)
		}
		if _, err := a.DB.SQL.ExecContext(ctx, `DELETE FROM tasks`); err != nil {
			return contracts.SettingsView{}, fmt.Errorf("clear tasks: %w", err)
		}
	}
	if err := a.Profiles.ResetToDefaults(ctx); err != nil {
		return contracts.SettingsView{}, fmt.Errorf("reset profiles: %w", err)
	}
	if keys, err := a.APIKeys.List(ctx); err == nil {
		for _, key := range keys {
			_ = a.APIKeys.Revoke(ctx, key.ID)
		}
	}
	if deleteModels && a.Models != nil {
		installed, err := a.Models.List(ctx)
		if err == nil {
			for _, m := range installed {
				if m.Installed {
					_ = a.Models.Delete(ctx, m.ID)
				}
			}
		}
	}
	a.Logger.Info("application reset to defaults", "delete_models", deleteModels)
	return a.settingsView(ctx)
}

func (a *App) exportDiagnostics(ctx context.Context, includeConversations bool) (string, error) {
	cfg := a.Config.Get()
	path := diagnostics.DefaultBundlePath(cfg)
	var hwJSON []byte
	if inv, err := a.hw.Detect(ctx); err == nil {
		hwJSON, _ = json.MarshalIndent(inv, "", "  ")
	}
	if err := diagnostics.WriteBundle(path, diagnostics.Options{
		IncludeConversations: includeConversations,
		Config:               cfg,
		HardwareJSON:         hwJSON,
	}); err != nil {
		return "", err
	}
	return path, nil
}

// DataDirHint returns a short path for logs.
func DataDirHint() string {
	return filepath.Clean(config.DefaultDataDir())
}
