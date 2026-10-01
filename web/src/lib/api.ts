import type {
  AIProfile,
  APIKeyPermissions,
  APIKeyRecord,
  BrowseModel,
  ChatRequest,
  ChatResponse,
  Conversation,
  CreateAPIKeyResponse,
  CreateConversationRequest,
  DiagnosticsExportResult,
  GenerationRun,
  BenchmarkJob,
  BenchmarkRequest,
  BenchmarkWorkload,
  HardwareInventory,
  HealthResponse,
  InstallFromURLRequest,
  LogContent,
  LogEntry,
  Message,
  Connector,
  NotificationList,
  ToolActivityRecord,
  ToolRecord,
  Model,
  ModelsFitResponse,
  Node,
  PairingSession,
  Recommendation,
  RunningModelView,
  RuntimeInfo,
  SettingsPatch,
  SettingsView,
  Task,
  Automation,
  AutomationDetail,
  AutomationInput,
  AutomationPreview,
  AutomationRun,
  UpdateConversationRequest,
  VersionResponse,
  BaseModelChoice,
  ClassifyResult,
  DatasetStats,
  EvalPrompt,
  KnowledgeHit,
  KnowledgeSource,
  MaterialUse,
  SpecializedAI,
  SpecializedAIPatch,
  SpecializedAIView,
  TrainingBackend,
  TrainingExample,
  TrainingJob,
  TrainingMaterial,
  TrainingMessage,
  TrainingPlan,
  TrainingPreset,
  SampleFile,
  MemoryCategory,
  MemoryItem,
  Artifact,
  FileRef,
  StopChatResponse,
} from '@/types/api'
import type { Upload } from '@/lib/upload'

export class ApiError extends Error {
  constructor(
    public readonly status: number,
    message: string,
    public readonly code?: string,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

function strField(raw: Record<string, unknown>, snake: string, pascal: string): string {
  const a = raw[snake]
  const b = raw[pascal]
  if (typeof a === 'string' && a) return a
  if (typeof b === 'string' && b) return b
  if (typeof a === 'string') return a
  if (typeof b === 'string') return b
  return ''
}

/** Accepts snake_case (current) or PascalCase (older daemon) pairing payloads. */
export function normalizePairingSession(raw: Record<string, unknown>): PairingSession {
  return {
    id: strField(raw, 'id', 'ID'),
    local_node_id: strField(raw, 'local_node_id', 'LocalNodeID'),
    remote_node_id: strField(raw, 'remote_node_id', 'RemoteNodeID'),
    remote_name: strField(raw, 'remote_name', 'RemoteName'),
    remote_address: strField(raw, 'remote_address', 'RemoteAddr') || undefined,
    code: strField(raw, 'code', 'Code'),
    state: strField(raw, 'state', 'State'),
    created_at: strField(raw, 'created_at', 'CreatedAt'),
    expires_at: strField(raw, 'expires_at', 'ExpiresAt'),
    incoming: Boolean(raw.incoming ?? raw.Incoming),
  }
}

const storedApiKeyName = 'yggdrasil.apiKey'
const storedApiKeyIdName = 'yggdrasil.apiKeyId'

export function storedApiKey(): string {
  if (typeof window === 'undefined') return ''
  return window.localStorage.getItem(storedApiKeyName) ?? ''
}

export function rememberApiKey(secret: string, id?: string) {
  if (typeof window === 'undefined') return
  window.localStorage.setItem(storedApiKeyName, secret)
  if (id) window.localStorage.setItem(storedApiKeyIdName, id)
}

export function forgetApiKey(id?: string) {
  if (typeof window === 'undefined') return
  if (id && window.localStorage.getItem(storedApiKeyIdName) !== id) return
  window.localStorage.removeItem(storedApiKeyName)
  window.localStorage.removeItem(storedApiKeyIdName)
}

function authHeaders(): Record<string, string> {
  const key = storedApiKey()
  if (!key) return {}
  return { Authorization: `Bearer ${key}` }
}

export function getApiBase(): string {
  if (typeof window === 'undefined') {
    return ''
  }
  const w = window as YggdrasilWindow
  return w.__YGGDRASIL_API_BASE__ ?? ''
}

async function parseJson<T>(response: Response): Promise<T | null> {
  const text = await response.text()
  if (!text) {
    return null
  }
  return JSON.parse(text) as T
}

async function request<T>(path: string, init?: RequestInit): Promise<T | null> {
  const url = `${getApiBase()}${path}`

  let response: Response
  try {
    response = await fetch(url, {
      ...init,
      headers: {
        Accept: 'application/json',
        ...authHeaders(),
        ...(init?.body ? { 'Content-Type': 'application/json' } : {}),
        ...init?.headers,
      },
    })
  } catch (err) {
    const detail = err instanceof Error ? err.message : 'network error'
    throw new ApiError(
      0,
      `Could not reach the local Yggdrasil service (${detail}). If this is the desktop app, quit and reopen it so the daemon restarts.`,
    )
  }

  if (response.status === 404) {
    return null
  }

  if (!response.ok) {
    const body = await parseJson<{ error?: { code?: string; message?: string } }>(response)
    throw new ApiError(
      response.status,
      body?.error?.message ?? response.statusText ?? `HTTP ${response.status}`,
      body?.error?.code,
    )
  }

  if (response.status === 204) {
    return null
  }

  return parseJson<T>(response)
}

export async function endpointExists(path: string): Promise<boolean> {
  try {
    const response = await fetch(`${getApiBase()}${path}`, {
      method: 'HEAD',
      headers: authHeaders(),
    })
    return response.ok
  } catch {
    return false
  }
}

export interface StreamChatOptions {
  body: ChatRequest
  signal?: AbortSignal
  onToken: (content: string) => void
  onDone?: () => void
  onError?: (message: string) => void
}

/** Save a stored file to the user's computer. It is fetched with the same
 * credentials as other API calls, so it works when the API needs a key. */
export async function downloadArtifact(file: Pick<FileRef, 'id' | 'name'>): Promise<void> {
  const response = await fetch(`${getApiBase()}/api/v1/artifacts/${file.id}/content`, { headers: authHeaders() })
  if (!response.ok) {
    throw new ApiError(response.status, response.status === 404 ? 'This file is no longer available.' : response.statusText)
  }
  const url = URL.createObjectURL(await response.blob())
  const link = document.createElement('a')
  link.href = url
  link.download = file.name
  document.body.appendChild(link)
  link.click()
  link.remove()
  setTimeout(() => URL.revokeObjectURL(url), 10_000)
}

export async function streamChat({
  body,
  signal,
  onToken,
  onDone,
  onError,
}: StreamChatOptions): Promise<void> {
  const url = `${getApiBase()}/api/v1/chat`
  const response = await fetch(url, {
    method: 'POST',
    headers: {
      Accept: 'text/event-stream',
      'Content-Type': 'application/json',
      ...authHeaders(),
    },
    body: JSON.stringify({ ...body, stream: true }),
    signal,
  })

  if (!response.ok) {
    const errBody = await parseJson<{ error?: { message?: string } }>(response)
    throw new ApiError(
      response.status,
      errBody?.error?.message ?? response.statusText,
    )
  }

  const reader = response.body?.getReader()
  if (!reader) {
    throw new ApiError(500, 'Streaming not supported')
  }

  const decoder = new TextDecoder()
  let buffer = ''
  let currentEvent = 'message'

  while (true) {
    const { done, value } = await reader.read()
    if (done) {
      break
    }

    buffer += decoder.decode(value, { stream: true })
    const lines = buffer.split('\n')
    buffer = lines.pop() ?? ''

    for (const line of lines) {
      if (line.startsWith('event:')) {
        currentEvent = line.slice(6).trim()
        continue
      }
      if (!line.startsWith('data:')) {
        continue
      }
      const data = line.slice(5).trim()
      if (currentEvent === 'error') {
        onError?.(data)
        return
      }
      if (currentEvent === 'token') {
        try {
          const parsed = JSON.parse(data) as { content?: string }
          if (parsed.content) {
            onToken(parsed.content)
          }
        } catch {
          // ignore malformed token payloads
        }
      }
      if (currentEvent === 'done') {
        onDone?.()
        return
      }
    }
  }

  onDone?.()
}

export const api = {
  getHealth: () => request<HealthResponse>('/api/v1/health'),

  getVersion: () => request<VersionResponse>('/api/v1/version'),

  getHardware: () => request<HardwareInventory>('/api/v1/hardware'),

  getModels: () => request<Model[]>('/api/v1/models'),

  recommendModels: (purpose: string) =>
    request<Recommendation>(`/api/v1/models/recommend?purpose=${encodeURIComponent(purpose)}`),

  getModelsFit: () => request<ModelsFitResponse[]>('/api/v1/models/fit'),

  browseModels: (q = '', limit = 24) => {
    const params = new URLSearchParams()
    if (q) params.set('q', q)
    params.set('limit', String(limit))
    return request<BrowseModel[]>(`/api/v1/models/browse?${params}`)
  },

  listRunningModels: () => request<RunningModelView[]>('/api/v1/models/running'),

  installModel: (id: string, opts?: { wait?: boolean; node_id?: string }) => {
    const params = new URLSearchParams()
    if (opts?.wait) params.set('wait', 'true')
    if (opts?.node_id) params.set('node_id', opts.node_id)
    const qs = params.toString()
    return request<{ status: string; model_id: string }>(
      `/api/v1/models/${id}/install${qs ? `?${qs}` : ''}`,
      {
        method: 'POST',
        body: opts?.node_id ? JSON.stringify({ node_id: opts.node_id }) : undefined,
      },
    )
  },

  installModelFromURL: (body: InstallFromURLRequest) =>
    request<{ status: string; model_id: string }>('/api/v1/models/install-from-url', {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  startModel: (id: string, nodeId?: string) =>
    request<RunningModelView>(`/api/v1/models/${id}/start`, {
      method: 'POST',
      body: JSON.stringify({ node_id: nodeId ?? '' }),
    }),

  stopModel: (id: string, opts?: { instance_id?: string; node_id?: string }) =>
    request<{ ok: boolean }>(`/api/v1/models/${id}/stop`, {
      method: 'POST',
      body: JSON.stringify({
        instance_id: opts?.instance_id ?? '',
        node_id: opts?.node_id ?? '',
      }),
    }),

  deleteModel: (id: string, opts?: { node_id?: string }) => {
    const params = new URLSearchParams()
    if (opts?.node_id) params.set('node_id', opts.node_id)
    const qs = params.toString()
    return request<null>(`/api/v1/models/${id}${qs ? `?${qs}` : ''}`, {
      method: 'DELETE',
      body: opts?.node_id ? JSON.stringify({ node_id: opts.node_id }) : undefined,
    })
  },

  listRuntimes: () => request<RuntimeInfo[]>('/api/v1/runtimes'),

  installRuntime: (id: string) =>
    request<{ status: string; runtime_id: string }>(`/api/v1/runtimes/${id}/install`, {
      method: 'POST',
    }),

  getProfiles: () => request<AIProfile[]>('/api/v1/profiles'),

  createProfile: (profile: Omit<AIProfile, 'id'> & { id?: string }) =>
    request<AIProfile>('/api/v1/profiles', {
      method: 'POST',
      body: JSON.stringify(profile),
    }),

  updateProfile: (id: string, profile: Partial<AIProfile> & { roles?: AIProfile['roles'] }) =>
    request<AIProfile>(`/api/v1/profiles/${id}`, {
      method: 'PATCH',
      body: JSON.stringify(profile),
    }),

  deleteProfile: (id: string) =>
    request<null>(`/api/v1/profiles/${id}`, { method: 'DELETE' }),

  listTools: () => request<ToolRecord[]>('/api/v1/tools'),
  toolActivity: () => request<ToolActivityRecord[]>('/api/v1/tools/activity'),
  setToolEnabled: (id: string, enabled: boolean) =>
    request<{ id: string; enabled: boolean }>(`/api/v1/tools/${encodeURIComponent(id)}/enabled`, {
      method: 'POST',
      body: JSON.stringify({ enabled }),
    }),
  testTool: (id: string, args: Record<string, unknown>) =>
    request<Record<string, unknown>>(`/api/v1/tools/${encodeURIComponent(id)}/test`, {
      method: 'POST',
      body: JSON.stringify({ args }),
    }),

  decideTool: (body: {
    request_id: string
    allow: boolean
    allow_session?: boolean
  }) =>
    request<{ status: string }>('/api/v1/tools/decide', {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  getNodes: () => request<Node[]>('/api/v1/nodes'),

  refreshNodes: () =>
    request<Node[]>('/api/v1/nodes/refresh', {
      method: 'POST',
      body: '{}',
    }),

  pairNode: async (nodeId: string) => {
    const raw = await request<Record<string, unknown>>('/api/v1/nodes/pair', {
      method: 'POST',
      body: JSON.stringify({ node_id: nodeId }),
    })
    return raw ? normalizePairingSession(raw) : null
  },

  listPairingOffers: async () => {
    const raw = await request<Record<string, unknown>[]>('/api/v1/nodes/pairing/pending')
    return (raw ?? []).map(normalizePairingSession)
  },

  claimPairing: async (nodeId: string, code: string) => {
    const raw = await request<Record<string, unknown>>('/api/v1/nodes/pair/claim', {
      method: 'POST',
      body: JSON.stringify({ node_id: nodeId, code }),
    })
    return raw ? normalizePairingSession(raw) : null
  },

  approvePairing: async (sessionId: string, code?: string) => {
    const raw = await request<Record<string, unknown>>(`/api/v1/nodes/${sessionId}/pair/approve`, {
      method: 'POST',
      body: JSON.stringify({ code: code ?? '' }),
    })
    return raw ? normalizePairingSession(raw) : null
  },

  revokeNode: (id: string) =>
    request<null>(`/api/v1/nodes/${id}/revoke`, { method: 'POST' }),

  listApiKeys: () => request<APIKeyRecord[]>('/api/v1/api-keys'),

  createApiKey: (name: string) =>
    request<CreateAPIKeyResponse>('/api/v1/api-keys', {
      method: 'POST',
      body: JSON.stringify({ name }),
    }),

  deleteApiKey: (id: string) =>
    request<null>(`/api/v1/api-keys/${id}`, { method: 'DELETE' }),

  setApiKeyPermissions: (id: string, permissions: APIKeyPermissions) =>
    request<APIKeyRecord>(`/api/v1/api-keys/${id}/permissions`, { method: 'PUT', body: JSON.stringify(permissions) }),

  rotateApiKey: (id: string) =>
    request<CreateAPIKeyResponse>(`/api/v1/api-keys/${id}/rotate`, {
      method: 'POST',
    }),

  getSettings: () => request<SettingsView>('/api/v1/settings'),

  updateSettings: (patch: SettingsPatch) =>
    request<SettingsView>('/api/v1/settings', {
      method: 'PATCH',
      body: JSON.stringify(patch),
    }),

  resetApp: (opts?: { delete_models?: boolean }) =>
    request<SettingsView>('/api/v1/settings/reset', {
      method: 'POST',
      body: JSON.stringify({ delete_models: Boolean(opts?.delete_models) }),
    }),

  getConversations: () => request<Conversation[]>('/api/v1/conversations'),

  createConversation: (body: CreateConversationRequest) =>
    request<Conversation>('/api/v1/conversations', {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  updateConversation: (id: string, body: UpdateConversationRequest) =>
    request<Conversation>(`/api/v1/conversations/${id}`, {
      method: 'PATCH',
      body: JSON.stringify(body),
    }),

  deleteConversation: (id: string) =>
    request<null>(`/api/v1/conversations/${id}`, {
      method: 'DELETE',
    }),

  getMessages: (conversationId: string) =>
    request<Message[]>(`/api/v1/conversations/${conversationId}/messages`),

  sendChat: (body: ChatRequest) =>
    request<ChatResponse>('/api/v1/chat', {
      method: 'POST',
      body: JSON.stringify({ ...body, stream: false }),
    }),

  listLogs: () => request<LogEntry[]>('/api/v1/logs'),

  getPerformance: (opts?: { sort?: string; order?: 'asc' | 'desc'; limit?: number }) => {
    const params = new URLSearchParams()
    if (opts?.sort) params.set('sort', opts.sort)
    if (opts?.order) params.set('order', opts.order)
    if (opts?.limit) params.set('limit', String(opts.limit))
    const qs = params.toString()
    return request<GenerationRun[]>(`/api/v1/performance${qs ? `?${qs}` : ''}`)
  },

  listTasks: () => request<Task[]>('/api/v1/tasks'),

  listAutomations: () => request<Automation[]>('/api/v1/automations'),

  getAutomation: (id: string) => request<AutomationDetail>(`/api/v1/automations/${id}`),

  previewAutomation: (body: AutomationInput) =>
    request<AutomationPreview>('/api/v1/automations/preview', {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  createAutomation: (body: AutomationInput) =>
    request<Automation>('/api/v1/automations', {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  updateAutomation: (id: string, body: Partial<AutomationInput>) =>
    request<Automation>(`/api/v1/automations/${id}`, {
      method: 'PATCH',
      body: JSON.stringify(body),
    }),

  deleteAutomation: (id: string) =>
    request<null>(`/api/v1/automations/${id}`, { method: 'DELETE' }),

  runAutomation: (id: string) =>
    request<AutomationRun>(`/api/v1/automations/${id}/run`, { method: 'POST' }),

  pauseAutomation: (id: string) =>
    request<Automation>(`/api/v1/automations/${id}/pause`, { method: 'POST' }),

  resumeAutomation: (id: string) =>
    request<Automation>(`/api/v1/automations/${id}/resume`, { method: 'POST' }),

  listBenchmarkWorkloads: () =>
    request<BenchmarkWorkload[]>('/api/v1/benchmarks/workloads'),

  listBenchmarks: () => request<BenchmarkJob[]>('/api/v1/benchmarks'),

  startBenchmark: (body: BenchmarkRequest) =>
    request<BenchmarkJob>('/api/v1/benchmarks', {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  getBenchmark: (id: string) => request<BenchmarkJob>(`/api/v1/benchmarks/${id}`),

  cancelBenchmark: (id: string) =>
    request<{ ok: boolean }>(`/api/v1/benchmarks/${id}/cancel`, { method: 'POST' }),

  getLog: (name: string, tailBytes = 262144) =>
    request<LogContent>(
      `/api/v1/logs/${encodeURIComponent(name)}?tail_bytes=${tailBytes}`,
    ),

  listMemory: () => request<{ memories: MemoryItem[]; categories: MemoryCategory[] }>('/api/v1/memory'),

  addMemory: (content: string, category?: MemoryCategory) =>
    request<MemoryItem>('/api/v1/memory', { method: 'POST', body: JSON.stringify({ content, category }) }),

  updateMemory: (id: string, body: { content?: string; category?: MemoryCategory; enabled?: boolean }) =>
    request<MemoryItem>(`/api/v1/memory/${id}`, { method: 'PATCH', body: JSON.stringify(body) }),

  deleteMemory: (id: string) => request<null>(`/api/v1/memory/${id}`, { method: 'DELETE' }),

  /** Upload a file to attach to a chat message. */
  uploadArtifact: (upload: Upload, conversationId?: string) =>
    request<Artifact>('/api/v1/artifacts', {
      method: 'POST',
      body: JSON.stringify({
        name: upload.filename,
        text: upload.text,
        content_base64: upload.contentBase64,
        conversation_id: conversationId,
      }),
    }),

  /** Stop a conversation's running turn on the computer running it; what was written is kept. */
  stopChat: (conversationId: string) =>
    request<StopChatResponse>('/api/v1/chat/stop', { method: 'POST', body: JSON.stringify({ conversation_id: conversationId }) }),

  listConnectors: async () => (await request<Connector[]>('/api/v1/connectors')) ?? [],

  /** Checks the values with the service, then stores them. A blank secret keeps the stored one. */
  connectService: (id: string, values: Record<string, string>) =>
    request<Connector>(`/api/v1/connectors/${id}`, { method: 'PUT', body: JSON.stringify({ values }) }),

  checkConnector: (id: string) => request<Connector>(`/api/v1/connectors/${id}/check`, { method: 'POST' }),

  disconnectService: (id: string) => request<null>(`/api/v1/connectors/${id}`, { method: 'DELETE' }),

  listNotifications: async (unreadOnly = false) =>
    (await request<NotificationList>(`/api/v1/notifications${unreadOnly ? '?unread=1' : ''}`)) ?? {
      notifications: [],
      unread: 0,
    },

  /** Marks notifications read; no ids marks every one read. */
  markNotificationsRead: (ids: string[] = []) =>
    request<null>('/api/v1/notifications/read', { method: 'POST', body: JSON.stringify({ ids }) }),

  dismissNotification: (id: string) => request<null>(`/api/v1/notifications/${id}/dismiss`, { method: 'POST' }),

  deleteArtifact: (id: string) => request<null>(`/api/v1/artifacts/${id}`, { method: 'DELETE' }),

  listKnowledge: () => request<KnowledgeSource[]>('/api/v1/knowledge/sources'),

  createKnowledge: (body: {
    name?: string
    kind: 'path' | 'text'
    path?: string
    filename?: string
    text?: string
    content_base64?: string
  }) =>
    request<KnowledgeSource>('/api/v1/knowledge/sources', { method: 'POST', body: JSON.stringify(body) }),

  updateKnowledge: (id: string, body: { name?: string; text?: string }) =>
    request<KnowledgeSource>(`/api/v1/knowledge/sources/${id}`, { method: 'PATCH', body: JSON.stringify(body) }),

  knowledgeContent: (id: string) => request<{ text: string }>(`/api/v1/knowledge/sources/${id}/content`),

  refreshKnowledge: (id: string) =>
    request<KnowledgeSource>(`/api/v1/knowledge/sources/${id}/refresh`, { method: 'POST' }),

  deleteKnowledge: (id: string) => request<null>(`/api/v1/knowledge/sources/${id}`, { method: 'DELETE' }),

  searchKnowledge: (query: string, sourceIds?: string[]) =>
    request<KnowledgeHit[]>('/api/v1/knowledge/search', {
      method: 'POST',
      body: JSON.stringify({ query, source_ids: sourceIds }),
    }),

  trainingBackends: () => request<TrainingBackend[]>('/api/v1/training/backends'),

  baseModels: (goal: string) =>
    request<BaseModelChoice[]>(`/api/v1/training/base-models?goal=${encodeURIComponent(goal)}`),

  classifyMaterial: (filename: string, text: string, use?: MaterialUse, contentBase64?: string) =>
    request<ClassifyResult>('/api/v1/training/classify', {
      method: 'POST',
      body: JSON.stringify({ filename, text, use, content_base64: contentBase64 }),
    }),

  listAIs: () => request<SpecializedAI[]>('/api/v1/training/ais'),

  trainingSamples: () => request<SampleFile[]>('/api/v1/training/samples'),

  createExampleAI: () => request<SpecializedAI>('/api/v1/training/example', { method: 'POST' }),

  listDeployedAIs: () => request<Model[]>('/api/v1/training/deployed'),

  createAI: (body: { name: string; goal: string; instructions?: string; base_model_id?: string; preset?: TrainingPreset }) =>
    request<SpecializedAI>('/api/v1/training/ais', { method: 'POST', body: JSON.stringify(body) }),

  getAI: (id: string) => request<SpecializedAIView>(`/api/v1/training/ais/${id}`),

  updateAI: (id: string, body: SpecializedAIPatch) =>
    request<SpecializedAI>(`/api/v1/training/ais/${id}`, { method: 'PATCH', body: JSON.stringify(body) }),

  deleteAI: (id: string) => request<null>(`/api/v1/training/ais/${id}`, { method: 'DELETE' }),

  addMaterial: (id: string, body: { name?: string; filename: string; text: string; use?: MaterialUse; content_base64?: string }) =>
    request<TrainingMaterial>(`/api/v1/training/ais/${id}/materials`, { method: 'POST', body: JSON.stringify(body) }),

  deleteMaterial: (id: string, materialId: string) =>
    request<null>(`/api/v1/training/ais/${id}/materials/${materialId}`, { method: 'DELETE' }),

  addConversations: (id: string, conversationIds: string[]) =>
    request<TrainingMaterial>(`/api/v1/training/ais/${id}/conversations`, {
      method: 'POST',
      body: JSON.stringify({ conversation_ids: conversationIds }),
    }),

  listExamples: (id: string) =>
    request<{ examples: TrainingExample[]; stats: DatasetStats }>(`/api/v1/training/ais/${id}/examples`),

  addExample: (id: string, messages: TrainingMessage[]) =>
    request<null>(`/api/v1/training/ais/${id}/examples`, { method: 'POST', body: JSON.stringify({ messages }) }),

  updateExample: (id: string, exampleId: string, body: { messages?: TrainingMessage[]; excluded?: boolean }) =>
    request<null>(`/api/v1/training/ais/${id}/examples/${exampleId}`, { method: 'PATCH', body: JSON.stringify(body) }),

  deleteExample: (id: string, exampleId: string) =>
    request<null>(`/api/v1/training/ais/${id}/examples/${exampleId}`, { method: 'DELETE' }),

  trainingPlan: (id: string) => request<TrainingPlan>(`/api/v1/training/ais/${id}/plan`),

  startTraining: (id: string, nodeId?: string) =>
    request<TrainingJob>(`/api/v1/training/ais/${id}/train`, {
      method: 'POST',
      ...(nodeId ? { body: JSON.stringify({ node_id: nodeId }) } : {}),
    }),

  cancelTraining: (jobId: string) => request<TrainingJob>(`/api/v1/training/jobs/${jobId}/cancel`, { method: 'POST' }),

  setTestPrompts: (id: string, prompts: string[]) =>
    request<EvalPrompt[]>(`/api/v1/training/ais/${id}/test-prompts`, { method: 'PUT', body: JSON.stringify({ prompts }) }),

  evaluateRevision: (id: string, revision: number) =>
    request<{ status: string }>(`/api/v1/training/ais/${id}/revisions/${revision}/evaluate`, { method: 'POST' }),

  deployRevision: (id: string, revision: number) =>
    request<SpecializedAI>(`/api/v1/training/ais/${id}/revisions/${revision}/deploy`, { method: 'POST' }),

  undeployAI: (id: string) => request<SpecializedAI>(`/api/v1/training/ais/${id}/undeploy`, { method: 'POST' }),

  exportDiagnostics: (includeConversations = false) =>
    request<DiagnosticsExportResult>('/api/v1/diagnostics', {
      method: 'POST',
      body: JSON.stringify({ include_conversations: includeConversations }),
    }),
}
