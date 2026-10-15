import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Fragment, useCallback, useEffect, useMemo, useRef, useState, type MouseEvent } from 'react'
import { Trans, useTranslation } from 'react-i18next'
import { Link, useLocation, useSearchParams } from 'react-router-dom'
import i18n from '@/i18n'
import { formatNumber } from '@/i18n/format'
import { RealmKicker } from '@/components/ui/Realm'
import { Ratatoskr } from '@/components/ui/Ratatoskr'
import { useMascotState } from '@/lib/ratatoskr/useMascotState'
import { useRole } from '@/lib/role'
import { ApiError, api, streamChat } from '@/lib/api'
import { attachAccept, MAX_ATTACH_BYTES, isAttachable, readUpload } from '@/lib/upload'
import { useCapabilities } from '@/lib/capabilities'
import { subscribeEvents } from '@/lib/events'
import { useDialog } from '@/lib/useDialog'
import { useUIStore } from '@/stores/uiStore'
import type {
  AIProfile,
  ChatTokenPayload,
  Conversation,
  FileRef,
  Message,
  OrchestrationRolePayload,
  ToolRequestedPayload,
} from '@/types/api'
import { capabilityGap, type CapabilityGap } from '@/features/models/capabilityGap'
import { activeCapabilities, capabilityLabel, setCapability } from '@/features/profiles/capabilities'
import { isTeamProfile } from '@/features/profiles/profilePresentation'
import { canChat, modelToolAssessment } from '@/features/models/modelPresentation'
import { CapabilityNotice } from './CapabilityNotice'
import { ChatActivity } from './ChatActivity'
import { ChatHistoryDrawer, useCanPinChatHistory } from './ChatHistoryDrawer'
import { AnswerDetails } from './AnswerDetails'
import { FileChip, PendingFileChip, type PendingFile } from './FileChips'
import { MemoryToggle } from './MemoryToggle'
import { AnswerActions, EditAction, VersionSwitch } from './MessageActions'
import { ReadAloudButton, SystemReadAloudButton } from './ReadAloud'
import { hasSystemSpeech } from './speakableText'
import { SetupOfferCard } from './SetupOffer'
import { AutomationDraftCard, AutomationRunNote } from './AutomationDraftCard'
import { ChatErrorCard } from './ChatErrorCard'
import { ChatMarkdown } from './ChatMarkdown'
import { ContextUsageButton } from './ContextUsageButton'
import { SummaryDialog } from './SummaryDialog'
import { contextWindow, parseContextUsage, type ContextUsage } from './contextUsage'
import { displayChatText } from './displayChatText'
import { parseModelFailure, type ModelFailure } from './modelFailure'
import { ModelFailureNotice } from './ModelFailureNotice'
import { toolDisplayName } from './toolNames'
import { useChatFollow } from './useChatFollow'
import { chatToResume, lastContextUsage } from './resume'
import { RatingDialogHost, RatingPrompt } from '@/features/models/ratings'
import { Skeleton } from '@/components/ui/Skeleton'
import { YggdrasilMark } from '@/components/ui/YggdrasilMark'
import { moveStored } from '@/lib/storage'
import { shownError, type Shown } from '@/components/ui/ShownError'

type TeamStep = {
  role: string
  nodeId?: string
  nodeName?: string
}

type PendingToolPrompt = {
  requestId: string
  toolId: string
  reason?: string
  argsSummary: string
  rawArgs?: string
}

type RunMode = 'automatic' | 'local'

/** How much work a message gets (spec §15). Auto lets Yggdrasil decide. */
type Effort = 'auto' | 'fast' | 'balanced' | 'thorough'
const EFFORT_KEY = 'toskar.chat.effort'
moveStored('ygg.chat.effort', EFFORT_KEY)
// The efforts, in order; their names and hints are chat:effort.<id> in the catalog.
const EFFORTS: Effort[] = ['auto', 'fast', 'balanced', 'thorough']

function savedEffort(): Effort {
  try {
    const v = localStorage.getItem(EFFORT_KEY)
    return EFFORTS.some((e) => e === v) ? (v as Effort) : 'auto'
  } catch {
    return 'auto'
  }
}

/** The Model choice that lets Yggdrasil pick an installed model for each message. */
const AUTO_MODEL_ID = 'auto'
// How long a deleted chat can be brought back with Undo (#452).
const UNDO_MS = 10_000

// The landing page's suggestions; each label and prompt is chat:landing.suggestions.<id>.
const SUGGESTIONS = ['explain', 'code', 'plan'] as const

const PROFILE_PURPOSE_ORDER = ['general', 'coding', 'research', 'custom'] as const

function sortProfiles(profiles: AIProfile[]): AIProfile[] {
  return [...profiles].sort((a, b) => {
    const ai = PROFILE_PURPOSE_ORDER.indexOf(
      a.purpose as (typeof PROFILE_PURPOSE_ORDER)[number],
    )
    const bi = PROFILE_PURPOSE_ORDER.indexOf(
      b.purpose as (typeof PROFILE_PURPOSE_ORDER)[number],
    )
    const ax = ai === -1 ? 99 : ai
    const bx = bi === -1 ? 99 : bi
    if (ax !== bx) return ax - bx
    return a.name.localeCompare(b.name)
  })
}

/** About how long a load takes, in whole seconds: "about 20 s" (#498). */
function aboutSeconds(ms: number): string {
  return i18n.t('common:units.seconds', { value: formatNumber(Math.max(1, Math.round(ms / 1000))) })
}

function formatRoleLabel(role: string): string {
  if (!role) return i18n.t('chat:roles.role')
  // A plan's worker slots are "worker:1", "worker:2", …
  const worker = /^worker:(\d+)$/.exec(role)
  if (worker) return i18n.t('chat:roles.worker', { n: worker[1] })
  // Deliberate's drafts are "drafter:2", "drafter:3", … (#459).
  const drafter = /^drafter:(\d+)$/.exec(role)
  if (drafter) return i18n.t('chat:roles.drafter', { n: drafter[1] })
  if (role === 'judge') return i18n.t('chat:roles.judge')
  if (role === 'planner') return i18n.t('chat:roles.planner')
  if (role === 'reviewer') return i18n.t('chat:roles.reviewer')
  const slot = /^([a-z]+):(\d+)$/.exec(role)
  if (slot) return `${slot[1].charAt(0).toUpperCase()}${slot[1].slice(1)} ${slot[2]}`
  return role.charAt(0).toUpperCase() + role.slice(1)
}

function hostLabel(value: string): string {
  try {
    return new URL(value).hostname || value
  } catch {
    return value.length > 48 ? `${value.slice(0, 48)}…` : value
  }
}

function friendlyToolDetail(args: Record<string, unknown> | undefined): string {
  if (!args) return ''
  for (const key of ['query', 'url', 'path', 'command', 'message', 'revision']) {
    const value = args[key]
    if (typeof value === 'string' && value.trim()) return value.trim()
  }
  return ''
}

function formatToolArgs(args: Record<string, unknown> | undefined): string {
  if (!args || Object.keys(args).length === 0) {
    return i18n.t('chat:tools.noArguments')
  }
  try {
    const raw = JSON.stringify(args, null, 2)
    return raw.length > 400 ? `${raw.slice(0, 400)}…` : raw
  } catch {
    return i18n.t('chat:tools.argumentsUnavailable')
  }
}

function toolProgress(toolId?: string, summary?: string, slow = false): string {
  const t = i18n.getFixedT(null, 'chat')
  if (toolId === 'internet.search') return t('status.searchingWeb')
  if (toolId === 'internet.open') return summary ? t('status.readingHost', { host: hostLabel(summary) }) : t('status.readingPage')
  if (toolId === 'filesystem.read') return t('status.readingFile')
  if (toolId === 'filesystem.search') return t('status.searchingFiles')
  if (toolId === 'spreadsheet.analyze') return t('status.readingSpreadsheet')
  if (toolId === 'files.create') return t('status.creatingFile')
  if (toolId === 'code.execute') return t('status.runningCode')
  if (toolId === 'image.generate') return slow ? t('status.makingImageSlow') : t('status.makingImage')
  if (toolId === 'image.edit') return slow ? t('status.changingImageSlow') : t('status.changingImage')
  if (toolId === 'video.generate') return slow ? t('status.makingVideoSlow') : t('status.makingVideo')
  if (toolId === 'terminal') return t('status.runningCommand')
  if (toolId === 'git.status' || toolId === 'git.diff' || toolId === 'git.log' || toolId === 'git.show') {
    return t('status.checkingGit')
  }
  return toolId ? t('status.usingTool', { tool: toolDisplayName(toolId) }) : t('status.working')
}

// Each chat's last context reading, kept while the app is open so leaving
// Chat and coming back shows it rather than an empty gauge (#230).
const usageByChat = new Map<string, ContextUsage>()

/** The Toskar mark beside each reply, in the column's start gutter from sm up. */
function ReplyMark() {
  return (
    <span className="chat-reply-mark" aria-hidden>
      <YggdrasilMark size={18} />
    </span>
  )
}

export function ChatPage() {
  const { t } = useTranslation('chat')
  // Setting up and rating models and tools is for Admins, automations for
  // Members (#203); until the role is known, the Owner's view, but nothing
  // an Admin's is asked for.
  const { role, atLeast } = useRole()
  const admin = role === undefined || atLeast('admin')
  const member = role === undefined || atLeast('member')
  const queryClient = useQueryClient()
  const [searchParams, setSearchParams] = useSearchParams()
  const activeProfileId = useUIStore((s) => s.activeProfileId)
  const setActiveProfileId = useUIStore((s) => s.setActiveProfileId)
  const advancedMode = useUIStore((s) => s.advancedMode)
  const chatHistoryPinned = useUIStore((s) => s.chatHistoryPinned)
  const setChatHistoryPinned = useUIStore((s) => s.setChatHistoryPinned)
  const pinnedConversationIds = useUIStore((s) => s.pinnedConversationIds)
  const togglePinnedConversation = useUIStore((s) => s.togglePinnedConversation)
  const canPinHistory = useCanPinChatHistory()

  const setLastChat = useUIStore((s) => s.setLastChat)
  // Coming back to Chat soon after leaving reopens the chat that was open,
  // unless the address asks for a chat or a new one.
  const [selectedId, setSelectedId] = useState<string | null>(() =>
    searchParams.get('c') || searchParams.get('new') === '1' ? null : chatToResume(useUIStore.getState().lastChat, Date.now()),
  )
  // A reopened chat that was deleted elsewhere since is let go once the list loads.
  const resumedRef = useRef(selectedId)
  const selectedRef = useRef(selectedId)
  selectedRef.current = selectedId
  useEffect(() => {
    setLastChat(selectedId ? { id: selectedId, at: Date.now() } : null)
  }, [selectedId, setLastChat])
  // Leaving Chat starts the clock on reopening the chat.
  useEffect(
    () => () => {
      const id = selectedRef.current
      if (id) setLastChat({ id, at: Date.now() })
    },
    [setLastChat],
  )
  const location = useLocation()
  // Another page can start a chat with text ready to send, such as a tool
  // source's ready-made prompt.
  const [draft, setDraft] = useState(() => (location.state as { draft?: string } | null)?.draft ?? '')
  const [effort, setEffortState] = useState<Effort>(savedEffort)
  const setEffort = (next: Effort) => {
    setEffortState(next)
    try {
      localStorage.setItem(EFFORT_KEY, next)
    } catch {
      // Remembering the choice is a convenience; the chat works without it.
    }
  }
  // Files added to the composer for the next message.
  const [pendingFiles, setPendingFiles] = useState<PendingFile[]>([])
  const fileInputRef = useRef<HTMLInputElement>(null)
  const [draftProfileId, setDraftProfileId] = useState<string | null>(null)
  const [draftModelId, setDraftModelId] = useState<string | null>(null)
  const [modelChoice, setModelChoice] = useState<{ chatId: string | null; modelId: string } | null>(null)
  // The model Auto (or a fallback) used for the latest turn in a chat.
  const [routedModel, setRoutedModel] = useState<{ chatId: string; modelId: string } | null>(null)
  const [streamingContent, setStreamingContent] = useState<string | null>(null)
  const [isSending, setIsSending] = useState(false)
  const [statusMessage, setStatusMessage] = useState<string | null>(null)
  const [sendError, setSendError] = useState<string | null>(null)
  // The stable code of the error in sendError, kept with its text so a later error never takes it.
  const [sendErrorCode, setSendErrorCode] = useState<{ text: string; code: string } | null>(null)
  // Memory Off chosen before the conversation exists; applied when it is created.
  const [memoryOffDraft, setMemoryOffDraft] = useState(false)
  const [modelFailure, setModelFailure] = useState<ModelFailure | null>(null)
  const [responseInterrupted, setResponseInterrupted] = useState(false)
  const [capabilityNotice, setCapabilityNotice] = useState<CapabilityGap | null>(null)
  const [toolTraces, setToolTraces] = useState<{ label: string; detail: string; status: string }[]>([])
  const [toolDetailsOpen, setToolDetailsOpen] = useState(false)
  const [teamSteps, setTeamSteps] = useState<TeamStep[]>([])
  // The parts of a request Yggdrasil is working through, shown while it runs.
  const [planSteps, setPlanSteps] = useState<{ step: string; status: 'pending' | 'running' | 'done' | 'failed' }[]>([])
  const [pendingTool, setPendingTool] = useState<PendingToolPrompt | null>(null)
  const [toolDeciding, setToolDeciding] = useState(false)
  const [renamingId, setRenamingId] = useState<string | null>(null)
  const [renameValue, setRenameValue] = useState('')
  const [historyOpen, setHistoryOpen] = useState(false)
  // The chats a confirmation asks about: one from its menu, or several from
  // select mode (#452).
  const [pendingDelete, setPendingDelete] = useState<Conversation[] | null>(null)
  // A confirmed delete waits UNDO_MS for Undo before it's sent; its chats
  // are hidden meanwhile. reopen is the chat that was open, for Undo.
  const [undoable, setUndoable] = useState<{ ids: string[]; reopen: string | null } | null>(null)
  const undoRef = useRef<{ ids: string[]; reopen: string | null } | null>(null)
  const undoTimer = useRef<number | null>(null)
  const [hiddenIds, setHiddenIds] = useState<Set<string>>(() => new Set())
  const [listError, setListError] = useState<string | null>(null)
  const [runMode, setRunMode] = useState<RunMode | null>(null)
  const [executionAsked, setExecutionAsked] = useState(false)
  // Chat options (profile, run on, effort, memory) sit behind one button.
  const [optionsOpen, setOptionsOpen] = useState(false)
  const optionsRef = useRef<HTMLDivElement | null>(null)
  const optionsButtonRef = useRef<HTMLButtonElement | null>(null)
  useEffect(() => {
    if (!optionsOpen) return
    const onPointer = (event: globalThis.MouseEvent) => {
      if (!optionsRef.current?.contains(event.target as Node)) setOptionsOpen(false)
    }
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return
      setOptionsOpen(false)
      optionsButtonRef.current?.focus()
    }
    document.addEventListener('mousedown', onPointer)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onPointer)
      document.removeEventListener('keydown', onKey)
    }
  }, [optionsOpen])
  const { scrollerRef, contentRef, showJump, jumpToLatest, followLatest } = useChatFollow(selectedId)

  // Each chat keeps its last reading, so switching back shows it rather
  // than an empty gauge until the next answer (#230).
  useEffect(() => {
    setContextUsage((selectedId && usageByChat.get(selectedId)) || null)
  }, [selectedId])
  const abortRef = useRef<AbortController | null>(null)
  const lastUserMessageRef = useRef('')
  // The sent message being edited (#447): its text is in the box, and
  // sending makes a new version of it.
  const [editing, setEditing] = useState<string | null>(null)
  const [toolFailure, setToolFailure] = useState<string | null>(null)
  const [contextUsage, setContextUsage] = useState<ContextUsage | null>(null)
  const streamingConvRef = useRef<string | null>(null)
  // The picture or clip being made takes minutes here (chat.making_media).
  const mediaSlowRef = useRef(false)
  const composerRef = useRef<HTMLTextAreaElement | null>(null)
  const profileSelectRef = useRef<HTMLSelectElement | null>(null)
  const modelSelectRef = useRef<HTMLSelectElement | null>(null)
  const streamingTextRef = useRef('')
  // The turn that just finished. Its stream can still deliver tokens after
  // chat.complete (a memory reply finishes instantly); they must not draw a
  // second copy of the saved answer.
  const completedConvRef = useRef<string | null>(null)
  // Set when the user presses Stop, so the closed stream is not shown as an error.
  const stoppedRef = useRef(false)

  const conversationsQuery = useQuery({
    queryKey: ['conversations'],
    queryFn: () => api.getConversations(),
    retry: false,
  })

  const profilesQuery = useQuery({
    queryKey: ['profiles'],
    queryFn: () => api.getProfiles(),
    retry: false,
  })

  const modelsQuery = useQuery({
    queryKey: ['models'],
    queryFn: () => api.getModels(),
    retry: false,
  })

  const specializedQuery = useQuery({
    queryKey: ['training', 'deployed'],
    queryFn: () => api.listDeployedAIs(),
    retry: false,
  })

  const nodesQuery = useQuery({
    queryKey: ['nodes'],
    queryFn: () => api.getNodes(),
    retry: false,
    staleTime: 30_000,
  })

  const settingsQuery = useQuery({
    queryKey: ['settings'],
    queryFn: () => api.getSettings(),
    retry: false,
    staleTime: 60_000,
  })
  const toolsQuery = useQuery({
    queryKey: ['tools'],
    queryFn: () => api.listTools(),
    retry: false,
    staleTime: 60_000,
  })
  // What the person may use (#589): what's off isn't offered.
  const { allows } = useCapabilities()
  const accept = attachAccept({ files: allows('files'), pictures: allows('pictures_in'), voice: allows('voice') })
  // Read aloud runs on this computer, where speech can be installed.
  const speechTool = toolsQuery.data?.find((t) => t.id === 'speech.synthesize')
  const canReadAloud = allows('voice') && !!speechTool && speechTool.health !== 'unavailable'
  // Where it can't, such as the Mac App Store app, the device's own voices read instead.
  const systemReadAloud = allows('voice') && !!toolsQuery.data && !canReadAloud && hasSystemSpeech()

  const messagesQuery = useQuery({
    queryKey: ['messages', selectedId],
    queryFn: () => (selectedId ? api.getMessages(selectedId) : null),
    enabled: Boolean(selectedId),
    retry: false,
  })

  // A chat with no reading this session shows the one saved with its latest
  // answer (contract 1.6), so the gauge survives a restart too.
  useEffect(() => {
    if (!selectedId || usageByChat.has(selectedId)) return
    const saved = lastContextUsage(messagesQuery.data)
    if (!saved) return
    usageByChat.set(selectedId, saved)
    setContextUsage(saved)
  }, [selectedId, messagesQuery.data])

  useEffect(() => {
    const resumed = resumedRef.current
    const list = conversationsQuery.data
    if (!resumed || !list) return
    resumedRef.current = null
    if (selectedId === resumed && !list.some((c) => c.id === resumed)) setSelectedId(null)
  }, [conversationsQuery.data, selectedId])

  useEffect(() => {
    const fromQuery = searchParams.get('c')
    const profileFromQuery = searchParams.get('profile')
    const startNew = searchParams.get('new') === '1'
    if (!fromQuery && !profileFromQuery && !startNew) return

    if (fromQuery) {
      setSelectedId(fromQuery)
    }
    if (profileFromQuery) {
      setActiveProfileId(profileFromQuery)
      setDraftProfileId(profileFromQuery)
    }
    if (startNew) {
      setSelectedId(null)
      setSendError(null)
      setStreamingContent(null)
      setTeamSteps([])
      setPlanSteps([])
      setDraft('')
      setListError(null)
    }
    setSearchParams({}, { replace: true })
  }, [searchParams, setSearchParams, setActiveProfileId])

  const conversations = conversationsQuery.data ?? []
  const visibleConversations = useMemo(() => {
    const all = conversationsQuery.data ?? []
    return hiddenIds.size === 0 ? all : all.filter((c) => !hiddenIds.has(c.id))
  }, [conversationsQuery.data, hiddenIds])
  const selectedConversation =
    conversations.find((c) => c.id === selectedId) ?? null
  // Embedding, reranker, and classifier models help Yggdrasil but cannot chat.
  const installedModels = (modelsQuery.data ?? []).filter(
    (m) => (m.installed || (m.installed_on?.length ?? 0) > 0) && canChat(m),
  )
  // Deployed specialized AIs answer through their base model, on this computer.
  const specializedModels = (specializedQuery.data ?? []).filter((m) => m.installed)
  // A person pinned to profiles chats with only those (#345).
  const meQuery = useQuery({ queryKey: ['me'], queryFn: () => api.getMe(), staleTime: 60_000, retry: false })
  const allowedProfiles = meQuery.data?.profiles
  const sortedProfiles = useMemo(() => {
    const all = sortProfiles(profilesQuery.data ?? [])
    return allowedProfiles ? all.filter((p) => allowedProfiles.includes(p.id)) : all
  }, [profilesQuery.data, allowedProfiles])
  const mayUse = (id?: string | null) => (id && (!allowedProfiles || allowedProfiles.includes(id)) ? id : null)

  const hasCluster = useMemo(() => {
    const nodes = nodesQuery.data ?? []
    const online = nodes.filter((n) => n.status === 'online')
    if (online.length > 1) return true
    return nodes.some((n) => !n.is_local && (n.paired || n.status === 'online'))
  }, [nodesQuery.data])

  const preferredProfileId = settingsQuery.data?.default_profile_id
  const preferredExists = preferredProfileId
    ? sortedProfiles.some((p) => p.id === preferredProfileId)
    : false

  const defaultProfileId =
    mayUse(activeProfileId) ||
    (preferredExists ? preferredProfileId : null) ||
    (hasCluster
      ? sortedProfiles.find((p) => isTeamProfile(p))?.id ||
        sortedProfiles.find((p) => p.id === 'programming')?.id ||
        sortedProfiles.find((p) => p.purpose === 'coding')?.id
      : null) ||
    sortedProfiles.find((p) => p.id === 'general-assistant')?.id ||
    sortedProfiles.find((p) => p.purpose === 'general')?.id ||
    sortedProfiles[0]?.id ||
    null

  const profileIdForChat =
    mayUse(selectedConversation?.profile_id) ||
    mayUse(draftProfileId) ||
    defaultProfileId ||
    undefined

  const chosenModelId = modelChoice && modelChoice.chatId === selectedId ? modelChoice.modelId : null
  const modelIdForChat =
    chosenModelId ||
    selectedConversation?.model_id ||
    draftModelId ||
    (installedModels.length > 0 ? AUTO_MODEL_ID : undefined)
  const isAuto = modelIdForChat === AUTO_MODEL_ID
  // With Auto, the model shown is the one the latest turn used.
  const shownModelId = isAuto
    ? routedModel && routedModel.chatId === selectedId
      ? routedModel.modelId
      : null
    : modelIdForChat

  const modelLocations =
    installedModels.find((m) => m.id === shownModelId)?.installed_on ?? []

  const chatModel =
    [...(modelsQuery.data ?? []), ...specializedModels].find((model) => model.id === shownModelId) ?? null
  const activeProfile =
    sortedProfiles.find((p) => p.id === profileIdForChat) ?? null
  const terminalAllowed = activeProfile?.tools?.find((tool) => tool.tool_id === 'terminal')?.policy !== 'deny'
  const internetAllowed =
    activeProfile?.tools == null
      ? true
      : activeProfile.tools.some(
          (tool) =>
            (tool.tool_id === 'internet.search' || tool.tool_id === 'internet.open') &&
            tool.policy !== 'deny',
        )
  const chatToolAssessment = chatModel ? modelToolAssessment(chatModel, { terminalAllowed }) : null
  const capabilityLine = activeCapabilities(activeProfile?.tools).map((id) =>
    id === 'internet' ? t('composer.web') : capabilityLabel(id),
  )
  const activeIsTeam = isTeamProfile(activeProfile)

  const defaultExecution = settingsQuery.data?.default_execution ?? 'automatic'
  const downloadBehavior = settingsQuery.data?.download_behavior ?? 'ask'

  useEffect(() => {
    if (runMode != null) return
    if (defaultExecution === 'ask') return
    if (defaultExecution === 'local' || defaultExecution === 'automatic') {
      setRunMode(defaultExecution)
    }
  }, [defaultExecution, runMode])

  const effectiveRunMode: RunMode = runMode ?? (activeIsTeam ? 'automatic' : 'local')

  useEffect(() => {
    if (!draftProfileId && defaultProfileId) {
      setDraftProfileId(defaultProfileId)
    }
  }, [defaultProfileId, draftProfileId])

  useEffect(() => {
    if (!draftModelId && installedModels.length > 0) {
      setDraftModelId(AUTO_MODEL_ID)
    }
  }, [draftModelId, installedModels])

  useEffect(() => {
    composerRef.current?.focus()
    // An edit belongs to the chat it started in.
    setEditing(null)
  }, [selectedId])

  // Sends a delete once its Undo has passed. A chat that was already gone
  // comes back skipped, and is gone all the same.
  const commitDelete = useCallback(
    async (ids: string[], keepalive = false) => {
      try {
        await api.deleteConversations(ids, { keepalive })
        queryClient.setQueryData<Conversation[]>(['conversations'], (current) =>
          (current ?? []).filter((c) => !ids.includes(c.id)),
        )
        for (const id of ids) queryClient.removeQueries({ queryKey: ['messages', id] })
        queryClient.invalidateQueries({ queryKey: ['conversations'] })
      } catch (error) {
        setListError(error instanceof Error ? error.message : t('send.deleteFailed'))
      } finally {
        setHiddenIds((current) => new Set([...current].filter((id) => !ids.includes(id))))
      }
    },
    [queryClient, t],
  )

  // Makes the waiting delete final now: when another is confirmed, the
  // page closes, or its time is up.
  const finishPendingDelete = useCallback(
    (keepalive = false) => {
      const pending = undoRef.current
      if (undoTimer.current !== null) window.clearTimeout(undoTimer.current)
      undoTimer.current = null
      undoRef.current = null
      setUndoable(null)
      if (pending) void commitDelete(pending.ids, keepalive)
    },
    [commitDelete],
  )

  useEffect(() => {
    const flush = () => finishPendingDelete(true)
    window.addEventListener('pagehide', flush)
    return () => {
      window.removeEventListener('pagehide', flush)
      flush()
    }
  }, [finishPendingDelete])

  const updateConversation = useMutation({
    mutationFn: ({
      id,
      model_id,
      profile_id,
      title,
      memory_off,
      compact_off,
    }: {
      id: string
      model_id?: string
      profile_id?: string
      title?: string
      memory_off?: boolean
      compact_off?: boolean
    }) => api.updateConversation(id, { model_id, profile_id, title, memory_off, compact_off }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['conversations'] })
      setRenamingId(null)
    },
  })

  // Auto-compact (#342): the open chat's summary, compacting it now, and a
  // new chat from it.
  const summaryQuery = useQuery({
    queryKey: ['summary', selectedId],
    queryFn: () => api.getConversationSummary(selectedId!),
    enabled: !!selectedId,
    staleTime: 30_000,
    retry: false,
  })
  const [summaryOpen, setSummaryOpen] = useState(false)
  const [compactError, setCompactError] = useState<Shown | null>(null)
  const compactNow = useMutation({
    mutationFn: (id: string) => api.compactConversation(id),
    onSuccess: (next, id) => {
      setCompactError(null)
      if (next) queryClient.setQueryData(['summary', id], next)
    },
    onError: (err) => setCompactError(shownError(err)),
  })
  const newChatFromSummary = useMutation({
    mutationFn: (id: string) => api.conversationFromSummary(id),
    onSuccess: (created) => {
      if (!created) return
      void queryClient.invalidateQueries({ queryKey: ['conversations'] })
      setSelectedId(created.id)
    },
    onError: (err) => setCompactError(shownError(err)),
  })

  const handleChatComplete = useCallback(
    (conversationId: string) => {
      if (conversationId && conversationId === streamingConvRef.current) {
        setStreamingContent(null)
        streamingConvRef.current = null
        completedConvRef.current = conversationId
        setIsSending(false)
        setStatusMessage(null)
        setPendingTool(null)
        // The saved answer now carries its sources and steps.
        setToolTraces([])
        abortRef.current = null
        queryClient.invalidateQueries({ queryKey: ['messages', conversationId] })
        queryClient.invalidateQueries({ queryKey: ['conversations'] })
      }
    },
    [queryClient],
  )

  const decidePendingTool = async (allow: boolean, allowSession = false) => {
    if (!pendingTool || toolDeciding) return
    setToolDeciding(true)
    try {
      await api.decideTool({
        request_id: pendingTool.requestId,
        allow,
        allow_session: allowSession,
      })
      setPendingTool(null)
      setStatusMessage(
        allow ? (allowSession ? t('status.toolAllowedSession') : t('status.toolAllowed')) : t('status.toolDenied'),
      )
    } catch (error) {
      setSendError(
        error instanceof Error
          ? error.message
          : t('tools.decideFailed'),
      )
    } finally {
      setToolDeciding(false)
    }
  }

  // Both dialogs keep keyboard focus inside while open. Escape is the safe
  // choice: keep the chat, or deny the tool.
  const deleteDialogRef = useDialog(Boolean(pendingDelete), () => setPendingDelete(null))
  // A chat an automation posts its results to: deleting it stops the posts,
  // not the automation, which the confirmation says (#452).
  const automationsQuery = useQuery({
    queryKey: ['automations'],
    queryFn: () => api.listAutomations(),
    enabled: Boolean(pendingDelete),
    retry: false,
  })
  const automationChats = (pendingDelete ?? []).filter((c) =>
    (automationsQuery.data ?? []).some((a) => a.conversation_id === c.id),
  ).length
  const toolDialogRef = useDialog(Boolean(pendingTool), () => {
    if (!toolDeciding) void decidePendingTool(false)
  })

  // The event stream is opened once. Its handler reads the current chat,
  // model locations, and completion handler from this ref; depending on
  // them reopened the stream on nearly every render and dropped events.
  const eventContext = useRef({ selectedId, handleChatComplete, modelLocations })
  eventContext.current = { selectedId, handleChatComplete, modelLocations }

  useEffect(() => {
    const unsubscribe = subscribeEvents({
      onEvent: (event) => {
        const { selectedId, handleChatComplete, modelLocations } = eventContext.current
        if (event.type === 'tool.requested') {
          const payload = event.payload as ToolRequestedPayload | undefined
          if (!payload?.request_id || !payload.tool_id) return
          const conversationId = payload.conversation_id
          if (
            conversationId &&
            conversationId !== selectedId &&
            conversationId !== streamingConvRef.current
          ) {
            return
          }
          setPendingTool({
            requestId: payload.request_id,
            toolId: payload.tool_id,
            reason: payload.reason,
            argsSummary: friendlyToolDetail(payload.args) || t('tools.noDetails'),
            rawArgs: formatToolArgs(payload.args),
          })
          setStatusMessage(t('status.waitingForPermission', { tool: toolDisplayName(payload.tool_id) }))
        }
        if (event.type === 'tool.started') {
          const toolId = event.payload?.tool_id as string | undefined
          const summary = event.payload?.summary as string | undefined
          setStatusMessage(toolProgress(toolId, summary, mediaSlowRef.current))
          if (toolId) {
            setToolTraces((current) => [
              ...current,
              { label: toolDisplayName(toolId), detail: summary || '', status: 'running' },
            ])
          }
        }
        if (event.type === 'tool.completed') {
          const ms = Number(event.payload?.duration_ms)
          const done = Number.isFinite(ms) && ms >= 0 ? t('tools.completedIn', { ms: Math.round(ms) }) : t('tools.completed')
          setStatusMessage(t('status.checkingSources'))
          setToolFailure(null)
          setToolTraces((current) => {
            const next = [...current]
            for (let i = next.length - 1; i >= 0; i -= 1) {
              if (next[i].status === 'running') {
                next[i] = { ...next[i], status: done }
                break
              }
            }
            return next
          })
        }
        if (event.type === 'tool.failed') {
          if (event.payload?.malformed) return
          const toolId = event.payload?.tool_id as string | undefined
          const label = toolId ? toolDisplayName(toolId) : t('tools.unnamed')
          setToolFailure(label)
          setStatusMessage(t('status.toolFailed', { tool: label }))
          setToolTraces((current) => {
            const next = [...current]
            for (let i = next.length - 1; i >= 0; i -= 1) {
              if (next[i].status === 'running') {
                next[i] = { ...next[i], status: t('tools.traceFailed') }
                break
              }
            }
            return next
          })
        }
        // A turn that wouldn't fit compacts first (#342).
        if (event.type === 'chat.compacting') {
          const conversationId = event.payload?.conversation_id as string | undefined
          if (conversationId && (conversationId === selectedId || conversationId === streamingConvRef.current)) {
            setStatusMessage(t('compact.compacting'))
          }
        }
        if (event.type === 'chat.summarized') {
          const conversationId = event.payload?.conversation_id as string | undefined
          if (conversationId) void queryClient.invalidateQueries({ queryKey: ['summary', conversationId] })
        }
        if (event.type === 'orchestration.role') {
          const payload = event.payload as OrchestrationRolePayload | undefined
          const conversationId = payload?.conversation_id
          if (
            conversationId &&
            conversationId !== selectedId &&
            conversationId !== streamingConvRef.current
          ) {
            return
          }
          if (!payload?.role) return
          const nodeName =
            payload.node_name ||
            modelLocations.find((n) => n.node_id === payload.node_id)?.node_name ||
            payload.node_id
          setTeamSteps((current) => {
            if (current.some((step) => step.role === payload.role)) {
              return current.map((step) =>
                step.role === payload.role
                  ? { role: payload.role, nodeId: payload.node_id, nodeName }
                  : step,
              )
            }
            return [
              ...current,
              { role: payload.role, nodeId: payload.node_id, nodeName },
            ]
          })
          setStatusMessage(
            nodeName
              ? t('status.roleOn', { role: formatRoleLabel(payload.role), computer: nodeName })
              : t('status.roleRunning', { role: formatRoleLabel(payload.role) }),
          )
        }
        if (event.type === 'chat.model_routed') {
          const conversationId = event.payload?.conversation_id as string | undefined
          if (!conversationId || (conversationId !== selectedId && conversationId !== streamingConvRef.current)) return
          const routedId = event.payload?.model_id as string | undefined
          if (routedId) setRoutedModel({ chatId: conversationId, modelId: routedId })
          const name = (event.payload?.model_name as string | undefined) || routedId
          if (name) setStatusMessage(t(event.payload?.fallback ? 'status.switchingTo' : 'status.using', { model: name }))
        }
        if (event.type === 'plan.created' || event.type === 'plan.step' || event.type === 'chat.verifying') {
          const conversationId = event.payload?.conversation_id as string | undefined
          if (conversationId && conversationId !== selectedId && conversationId !== streamingConvRef.current) return
          if (event.type === 'chat.verifying') {
            setStatusMessage(t('status.verifying'))
          } else if (event.type === 'plan.created') {
            const steps = (event.payload?.steps as string[] | undefined) ?? []
            setPlanSteps(steps.map((step) => ({ step, status: 'pending' })))
            setStatusMessage(t('status.workingThrough', { count: steps.length }))
          } else {
            const index = Number(event.payload?.index)
            const status = event.payload?.status as 'running' | 'done' | 'failed'
            setPlanSteps((cur) => cur.map((s, i) => (i === index ? { ...s, status } : s)))
            if (status === 'running') setStatusMessage(t('status.workingOn', { step: event.payload?.step as string }))
            else setStatusMessage(t('status.puttingTogether'))
          }
        }
        if (event.type === 'chat.stopped') {
          const conversationId = event.payload?.conversation_id as string | undefined
          if (!conversationId) return
          // The kept part of the answer is saved; show it in every window.
          queryClient.invalidateQueries({ queryKey: ['messages', conversationId] })
          if (conversationId === streamingConvRef.current) {
            streamingConvRef.current = null
            completedConvRef.current = conversationId
            setIsSending(false)
            setStreamingContent(null)
            setStatusMessage(null)
            setPendingTool(null)
            setPlanSteps([])
          }
        }
        if (event.type === 'chat.making_file') {
          const conversationId = event.payload?.conversation_id as string | undefined
          if (conversationId && conversationId !== selectedId && conversationId !== streamingConvRef.current) return
          const name = event.payload?.name as string | undefined
          setStatusMessage(name ? t('status.writingNamed', { name }) : t('status.writingFile'))
        }
        // Toskar is making a picture or a clip the message asked for.
        if (event.type === 'chat.making_media') {
          const conversationId = event.payload?.conversation_id as string | undefined
          if (conversationId && conversationId !== selectedId && conversationId !== streamingConvRef.current) return
          const kind = event.payload?.kind as string | undefined
          // Without a GPU here, or short on memory, say it takes a while.
          const slow = event.payload?.slow === true
          mediaSlowRef.current = slow
          if (kind === 'video') setStatusMessage(slow ? t('status.makingVideoSlow') : t('status.makingVideo'))
          else if (kind === 'edit') setStatusMessage(slow ? t('status.changingImageSlow') : t('status.changingImage'))
          else setStatusMessage(slow ? t('status.makingImageSlow') : t('status.makingImage'))
        }
        if (event.type === 'chat.lookup') {
          const conversationId = event.payload?.conversation_id as string | undefined
          if (conversationId && conversationId !== selectedId && conversationId !== streamingConvRef.current) return
          const query = event.payload?.query as string | undefined
          setStatusMessage(query ? t('status.searchingWebFor', { query }) : t('status.searchingWeb'))
        }
        if (event.type === 'model.load.started') {
          const conversationId = event.payload?.conversation_id as string | undefined
          if (conversationId && conversationId !== selectedId && conversationId !== streamingConvRef.current) return
          const nodeId = event.payload?.node_id as string | undefined
          const role = event.payload?.role as string | undefined
          const nodeName =
            (event.payload?.node_name as string | undefined) ||
            modelLocations.find((n) => n.node_id === nodeId)?.node_name
          // A model already loaded starts answering at once (#498).
          if (event.payload?.cold === false) {
            setStatusMessage(nodeName ? t('status.generatingOn', { computer: nodeName }) : t('status.generating'))
            return
          }
          const model = event.payload?.model_name as string | undefined
          const expected = Number(event.payload?.expected_ms) || 0
          setStatusMessage(
            model && nodeName && expected > 0
              ? t('status.loadingNamedOnAbout', { model, computer: nodeName, time: aboutSeconds(expected) })
              : model && nodeName
                ? t('status.loadingNamedOn', { model, computer: nodeName })
                : role && nodeName
                  ? t('status.loadingRoleOn', { role: formatRoleLabel(role), computer: nodeName })
                  : nodeName
                    ? t('status.loadingModelOn', { computer: nodeName })
                    : t('status.loadingModel'),
          )
        }
        if (event.type === 'model.load.completed') {
          const nodeId = event.payload?.node_id as string | undefined
          const role = event.payload?.role as string | undefined
          const nodeName =
            (event.payload?.node_name as string | undefined) ||
            modelLocations.find((n) => n.node_id === nodeId)?.node_name
          setStatusMessage(
            role && nodeName
              ? t('status.roleGeneratingOn', { role: formatRoleLabel(role), computer: nodeName })
              : nodeName
                ? t('status.generatingOn', { computer: nodeName })
                : t('status.generating'),
          )
        }
        if (event.type === 'chat.token') {
          if (streamingConvRef.current) return
          const payload = event.payload as ChatTokenPayload | undefined
          if (!payload?.conversation_id || payload.conversation_id !== selectedId) {
            return
          }
          if (payload.conversation_id === completedConvRef.current) return
          setStatusMessage(null)
          if (payload.content) {
            setStreamingContent((current) => (current ?? '') + payload.content)
          }
        }
        // An automation made from this chat posted its result here (#204).
        if (event.type === 'automation.completed') {
          const conversationId = event.payload?.conversation_id as string | undefined
          if (conversationId) {
            void queryClient.invalidateQueries({ queryKey: ['messages', conversationId] })
            void queryClient.invalidateQueries({ queryKey: ['conversations'] })
          }
        }
        if (event.type === 'chat.complete') {
          const conversationId = event.payload?.conversation_id as string | undefined
          const nextUsage = parseContextUsage(event.payload?.context)
          if (nextUsage && conversationId) usageByChat.set(conversationId, nextUsage)
          if (
            nextUsage &&
            conversationId &&
            conversationId === (streamingConvRef.current ?? selectedId)
          ) {
            setContextUsage(nextUsage)
          }
          const answeredBy = event.payload?.model_id as string | undefined
          if (conversationId && answeredBy && conversationId === (streamingConvRef.current ?? selectedId)) {
            setRoutedModel({ chatId: conversationId, modelId: answeredBy })
          }
          if (conversationId) {
            // A turn finished that this tab did not start (another window),
            // so later turns in it may stream here again.
            if (conversationId !== streamingConvRef.current && conversationId === completedConvRef.current) {
              completedConvRef.current = null
            }
            handleChatComplete(conversationId)
            // The answer is saved by now; the stream's end can come first,
            // and a fetch then would show the version it replaced (#447).
            if (conversationId === selectedId) {
              queryClient.invalidateQueries({ queryKey: ['messages', conversationId] })
            }
          }
        }
        if (event.type === 'knowledge.retrieved') {
          const conversationId = event.payload?.conversation_id as string | undefined
          if (conversationId && conversationId !== selectedId && conversationId !== streamingConvRef.current) return
          const passages = Number(event.payload?.passages) || 0
          const names = (event.payload?.sources as string[] | undefined) ?? []
          const source = names[0] ?? t('status.yourKnowledge')
          setStatusMessage(
            passages === 0
              ? t('status.checkedKnowledge')
              : names.length > 1
                ? t('status.foundPassagesMore', { count: passages, source, more: names.length - 1 })
                : t('status.foundPassages', { count: passages, source }),
          )
        }
        if (event.type === 'chat.error') {
          setIsSending(false)
          setStreamingContent(null)
          streamingConvRef.current = null
          setStatusMessage(null)
          setPendingTool(null)
          setSendError((event.payload?.message as string) || t('send.generationFailed'))
        }
      },
    })
    return unsubscribe
    // queryClient and t are stable; everything else is read through eventContext.
  }, [queryClient, t])

  const setProfile = (profileId: string) => {
    setDraftProfileId(profileId)
    if (selectedId) {
      updateConversation.mutate({ id: selectedId, profile_id: profileId })
    }
  }

  const chooseRunMode = (mode: RunMode) => {
    setRunMode(mode)
    setExecutionAsked(true)
  }

  const enableInternet = async () => {
    if (!activeProfile) return
    const tools = setCapability(activeProfile.tools ?? [], 'internet', true)
    await api.updateProfile(activeProfile.id, { ...activeProfile, tools })
    await queryClient.invalidateQueries({ queryKey: ['profiles'] })
    setCapabilityNotice(null)
  }

  const setModel = (modelId: string) => {
    setModelChoice({ chatId: selectedId, modelId })
    setDraftModelId(modelId)
    if (selectedId) {
      updateConversation.mutate({ id: selectedId, model_id: modelId })
    }
  }

  const ensureModelReady = async (): Promise<string | null> => {
    if (modelIdForChat) return modelIdForChat
    const catalog = modelsQuery.data ?? []
    const candidate =
      catalog.find((m) => m.installed || (m.installed_on?.length ?? 0) > 0)?.id ||
      activeProfile?.roles?.find((r) => r.model_id)?.model_id ||
      catalog[0]?.id
    if (!candidate) return null

    const already =
      catalog.find((m) => m.id === candidate)?.installed ||
      (catalog.find((m) => m.id === candidate)?.installed_on?.length ?? 0) > 0
    if (already) return candidate

    if (downloadBehavior === 'ask') {
      const name =
        catalog.find((m) => m.id === candidate)?.display_name || candidate
      const ok = window.confirm(t('send.confirmDownload', { model: name }))
      if (!ok) return null
    }

    setStatusMessage(t('status.downloadingModel'))
    await api.installModel(candidate, { wait: true })
    await queryClient.invalidateQueries({ queryKey: ['models'] })
    setDraftModelId(candidate)
    return candidate
  }

  const addFiles = (list: FileList | File[]) => {
    for (const file of Array.from(list)) {
      const key = `${file.name}-${file.size}-${Math.random().toString(36).slice(2)}`
      const base: PendingFile = { key, name: file.name, size: file.size, status: 'uploading' }
      if (!isAttachable(file.name)) {
        setPendingFiles((cur) => [...cur, { ...base, status: 'error', error: t('attachments.unsupported') }])
        continue
      }
      if (file.size > MAX_ATTACH_BYTES) {
        setPendingFiles((cur) => [...cur, { ...base, status: 'error', error: t('attachments.tooLarge') }])
        continue
      }
      setPendingFiles((cur) => [...cur, base])
      void (async () => {
        try {
          const uploaded = await api.uploadArtifact(await readUpload(file), selectedId ?? undefined)
          if (!uploaded) throw new Error(t('attachments.addFailed'))
          setPendingFiles((cur) => cur.map((f) => (f.key === key ? { ...f, status: 'ready', file: uploaded } : f)))
        } catch (err) {
          const error = err instanceof Error ? err.message : t('attachments.addFailed')
          setPendingFiles((cur) => cur.map((f) => (f.key === key ? { ...f, status: 'error', error } : f)))
        }
      })()
    }
  }

  const removePendingFile = (key: string) => {
    setPendingFiles((cur) => {
      const gone = cur.find((f) => f.key === key)
      if (gone?.file) void api.deleteArtifact(gone.file.id).catch(() => undefined)
      return cur.filter((f) => f.key !== key)
    })
  }

  // A setup offered in an answer finishes the request once it is ready
  // (Gungnir §29); the card holds a stable callback to the latest send.
  const sendRef = useRef<(text: string, attachments?: string[]) => void>(() => {})
  useEffect(() => {
    sendRef.current = (text: string, attachments?: string[]) => {
      // The files are the ones already in the chat, found by id.
      const known = (queryClient.getQueryData<Message[]>(['messages', selectedId]) ?? []).flatMap((m) => m.meta?.files ?? [])
      const files = attachments?.flatMap((id) => known.filter((f) => f.id === id).slice(0, 1))
      void sendMessage(text, undefined, files && files.length > 0 ? files : undefined)
    }
  })
  const continueAfterSetup = useCallback((text: string, attachments?: string[]) => sendRef.current(text, attachments), [])

  /**
   * Sends a message, or with branch, answers from a point in the chat as a
   * new version there (#447): retryOf asks an answer's question again,
   * editOf sends the text in place of a sent message, and modelId answers
   * with another model.
   */
  const sendMessage = async (
    overrideText?: string,
    branch?: { retryOf?: string; editOf?: string; modelId?: string },
    resendFiles?: FileRef[],
  ) => {
    if (!branch && editing && overrideText == null) branch = { editOf: editing }
    const current = queryClient.getQueryData<Message[]>(['messages', selectedId]) ?? []
    const branchAt = branch?.retryOf ?? branch?.editOf
    const branchIndex = branchAt ? current.findIndex((m) => m.id === branchAt) : -1
    if (branch?.retryOf) {
      const question = branchIndex > 0 ? current[branchIndex - 1] : undefined
      if (!question || question.role !== 'user') return
      overrideText = question.content
    }
    const readyFiles = overrideText == null ? pendingFiles.filter((f) => f.status === 'ready' && f.file) : []
    if (overrideText == null && pendingFiles.some((f) => f.status === 'uploading')) {
      setSendError(t('send.waitForFiles'))
      return
    }
    // A file on its own is a request to look at it.
    const typed = (overrideText ?? draft).trim()
    const message = typed || (readyFiles.length > 0 ? t('send.summarizeFile', { count: readyFiles.length }) : '')
    if (!message || isSending) return

    if (defaultExecution === 'ask' && !executionAsked && runMode == null) {
      setSendError(t('send.chooseRunOn'))
      return
    }

    let modelId: string | undefined = branch?.modelId ?? modelIdForChat
    if (!modelId) {
      try {
        modelId = (await ensureModelReady()) ?? undefined
      } catch (error) {
        setSendError(
          error instanceof Error
            ? error.message
            : t('send.downloadFailed'),
        )
        setStatusMessage(null)
        return
      }
      if (!modelId) {
        setSendError(t('send.installFirst'))
        return
      }
    }

    lastUserMessageRef.current = message
    followLatest()
    if (!branch?.retryOf) {
      setDraft('')
      setEditing(null)
    }
    // A request sent again after a setup keeps the files it was sent with.
    const attachments = resendFiles ?? readyFiles.map((f) => f.file!)
    if (overrideText == null) setPendingFiles([])
    setIsSending(true)
    setSendError(null)
    setModelFailure(null)
    setResponseInterrupted(false)
    setToolFailure(null)
    const catalog = modelsQuery.data ?? []
    // Auto chooses a capable model itself, so there is nothing to warn about.
    const activeModel = modelId === AUTO_MODEL_ID ? null : (catalog.find((item) => item.id === modelId) ?? chatModel)
    setCapabilityNotice(capabilityGap(message, activeModel, catalog, { terminalAllowed, internetAllowed }))
    setToolTraces([])
    setToolDetailsOpen(false)
    setStatusMessage(t('status.starting'))
    setTeamSteps([])
    setPlanSteps([])
    setPendingTool(null)
    setStreamingContent('')
    streamingTextRef.current = ''
    completedConvRef.current = null
    stoppedRef.current = false

    let conversationId = selectedId
    try {
      if (!conversationId) {
        const created = await api.createConversation({
          title: message.length > 48 ? `${message.slice(0, 48)}…` : message,
          profile_id: profileIdForChat,
          model_id: modelId,
        })
        if (!created?.id) {
          throw new Error(t('send.startFailed'))
        }
        conversationId = created.id
        if (memoryOffDraft) {
          await api.updateConversation(created.id, { memory_off: true })
          setMemoryOffDraft(false)
        }
        setSelectedId(conversationId)
        await queryClient.invalidateQueries({ queryKey: ['conversations'] })
      }

      streamingConvRef.current = conversationId
      const controller = new AbortController()
      abortRef.current = controller

      // A new chat's first fetch can return before the message is saved and
      // wipe the message shown below; cancel it so the message stays.
      await queryClient.cancelQueries({ queryKey: ['messages', conversationId] })

      queryClient.setQueryData<Message[]>(['messages', conversationId], (current) => {
        // A retry or an edit ends the chat at its point until the answer
        // comes; the earlier version stays on the server.
        if (branchIndex >= 0) current = (current ?? []).slice(0, branchIndex)
        if (branch?.retryOf) return current ?? []
        const optimistic: Message = {
          id: `optimistic-${Date.now()}`,
          conversation_id: conversationId!,
          role: 'user',
          content: message,
          created_at: new Date().toISOString(),
          meta: attachments.length > 0 ? { files: attachments } : undefined,
        }
        return [...(current ?? []), optimistic]
      })

      await streamChat({
        body: {
          conversation_id: conversationId,
          profile_id: profileIdForChat,
          model_id: modelId,
          message,
          stream: true,
          execution: effectiveRunMode,
          effort,
          attachments: attachments.length > 0 ? attachments.map((f) => f.id) : undefined,
          retry_of: branch?.retryOf,
          edit_of: branch?.editOf,
        },
        signal: controller.signal,
        onToken: (content) => {
          if (streamingConvRef.current !== conversationId) return
          setStatusMessage(null)
          setToolFailure(null)
          streamingTextRef.current += content
          setStreamingContent((current) => (current ?? '') + content)
        },
        onDone: () => {
          if (streamingConvRef.current !== conversationId) return
          if (!streamingTextRef.current.trim()) {
            setIsSending(false)
            streamingConvRef.current = null
            setStatusMessage(null)
            setModelFailure({
              kind: 'model_health',
              reason: 'runtime_error',
              message: t('send.noReply'),
            })
            queryClient.invalidateQueries({ queryKey: ['messages', conversationId] })
            return
          }
          handleChatComplete(conversationId!)
        },
        onError: (errMessage, code) => {
          setIsSending(false)
          streamingConvRef.current = null
          setStatusMessage(null)
          if (stoppedRef.current) {
            queryClient.invalidateQueries({ queryKey: ['messages', conversationId] })
            return
          }
          const failure = parseModelFailure(errMessage)
          const partial = streamingTextRef.current.trim()
          if (failure) {
            setModelFailure(failure)
            setSendError(null)
            setResponseInterrupted(failure.interrupted || partial.length > 0)
            setStreamingContent(partial ? streamingTextRef.current : null)
          } else {
            setStreamingContent(null)
            setResponseInterrupted(false)
            setSendError(errMessage || t('send.couldNotRespond'))
            if (code && errMessage) setSendErrorCode({ text: errMessage, code })
          }
          queryClient.invalidateQueries({ queryKey: ['messages', conversationId] })
        },
      })
    } catch (error) {
      setIsSending(false)
      setStreamingContent(null)
      streamingConvRef.current = null
      setStatusMessage(null)
      // Stop closes this window's stream; that is not a failure. The kept
      // part of the answer arrives with chat.stopped.
      if (stoppedRef.current || (error instanceof DOMException && error.name === 'AbortError')) {
        if (conversationId) queryClient.invalidateQueries({ queryKey: ['messages', conversationId] })
        return
      }
      if (error instanceof ApiError && error.code) {
        // The card explains the code in the App language; the service's text goes under Details.
        setSendError(error.serviceMessage)
        setSendErrorCode({ text: error.serviceMessage, code: error.code })
      } else {
        setSendError(error instanceof Error ? error.message : t('send.unreachable'))
      }
      if (conversationId) {
        queryClient.invalidateQueries({ queryKey: ['messages', conversationId] })
      }
    }
  }

  const handleStop = () => {
    stoppedRef.current = true
    // Stop the run on the computer too, not only this window's stream (§67).
    const running = streamingConvRef.current ?? selectedId
    if (running) void api.stopChat(running).catch(() => undefined)
    abortRef.current?.abort()
    setIsSending(false)
    setStreamingContent(null)
    streamingConvRef.current = null
    setStatusMessage(null)
    setPendingTool(null)
    if (selectedId) {
      queryClient.invalidateQueries({ queryKey: ['messages', selectedId] })
    }
  }

  const handleDelete = (conversation: Conversation, event: MouseEvent) => {
    event.preventDefault()
    event.stopPropagation()
    setListError(null)
    // Native window.confirm is unreliable in the Wails WebView — use in-app confirm.
    setPendingDelete([conversation])
  }

  const confirmDelete = () => {
    if (!pendingDelete) return
    const ids = pendingDelete.map((c) => c.id)
    setPendingDelete(null)
    setListError(null)
    finishPendingDelete()
    setHiddenIds((current) => new Set([...current, ...ids]))
    const reopen = selectedId && ids.includes(selectedId) ? selectedId : null
    if (reopen) {
      setSelectedId(null)
      setSendError(null)
      setStreamingContent(null)
      setTeamSteps([])
      setPlanSteps([])
    }
    const entry = { ids, reopen }
    undoRef.current = entry
    setUndoable(entry)
    undoTimer.current = window.setTimeout(() => finishPendingDelete(), UNDO_MS)
  }

  const undoDelete = () => {
    const pending = undoRef.current
    if (!pending) return
    if (undoTimer.current !== null) window.clearTimeout(undoTimer.current)
    undoTimer.current = null
    undoRef.current = null
    setUndoable(null)
    setHiddenIds((current) => new Set([...current].filter((id) => !pending.ids.includes(id))))
    if (pending.reopen) setSelectedId(pending.reopen)
  }

  const startRename = (conversation: Conversation, event?: MouseEvent) => {
    event?.stopPropagation()
    setRenamingId(conversation.id)
    setRenameValue(conversation.title)
  }

  const commitRename = () => {
    if (!renamingId || !renameValue.trim()) {
      setRenamingId(null)
      return
    }
    updateConversation.mutate({ id: renamingId, title: renameValue.trim() })
  }

  const startNewChat = () => {
    setSelectedId(null)
    setSendError(null)
    setCapabilityNotice(null)
    setToolFailure(null)
    setToolTraces([])
    setToolDetailsOpen(false)
    setStreamingContent(null)
    setTeamSteps([])
    setPlanSteps([])
    setDraft('')
    setListError(null)
    if (defaultExecution === 'ask') {
      setRunMode(null)
      setExecutionAsked(false)
    }
    if (!(chatHistoryPinned && canPinHistory)) {
      setHistoryOpen(false)
    }
    composerRef.current?.focus()
  }

  useEffect(() => {
    if (chatHistoryPinned && canPinHistory) {
      setHistoryOpen(true)
    }
  }, [chatHistoryPinned, canPinHistory])

  const messages = messagesQuery.data ?? []

  // Retry and edit (#447).
  const startEdit = (message: Message) => {
    setEditing(message.id)
    setDraft(message.content)
    composerRef.current?.focus()
  }
  const cancelEdit = () => {
    setEditing(null)
    setDraft('')
  }
  const showVersion = async (id: string) => {
    if (!selectedId) return
    const shown = await api.showMessageVersion(selectedId, id)
    if (shown) queryClient.setQueryData(['messages', selectedId], shown)
  }
  const retryModels = [
    { id: AUTO_MODEL_ID, name: t('versions.auto') },
    ...installedModels.map((m) => ({ id: m.id, name: m.display_name || m.id })),
  ]
  const streamingText = streamingContent == null ? '' : displayChatText(streamingContent)
  const replyInProgress = isSending && streamingText.length === 0
  // Ratatoskr: thinking while a reply is written, delivering when it runs
  // on a paired computer. Only events for this chat, or with no chat, count.
  const replyMascot = useMascotState({
    busy: isSending,
    react: ['think', 'deliver'],
    localNodeId: nodesQuery.data?.find((n) => n.is_local)?.id,
    accept: (event) => {
      const conv = event.payload?.conversation_id as string | undefined
      return !conv || conv === selectedId || conv === streamingConvRef.current
    },
  })
  const showLanding = !selectedId && !draft.trim() && !isSending
  // On the landing page the hero's mascot shows a problem, so the cards below
  // it go without one (one mascot per view).
  const landingError = showLanding && Boolean(modelFailure || sendError)
  const historyMode = chatHistoryPinned && canPinHistory ? 'pinned' : 'overlay'
  const showPinnedSidebar = historyOpen && historyMode === 'pinned'

  const runOnTitle = effectiveRunMode === 'automatic' ? t('composer.runOnAutomatic') : t('composer.runOnLocal')
  // Run on stays in view while Settings asks for a choice before sending.
  const needsRunOn = defaultExecution === 'ask' && !executionAsked && runMode == null
  const memoryOff = selectedConversation ? Boolean(selectedConversation.memory_off) : memoryOffDraft
  // The Options button names what differs from the defaults, so the chat's
  // setup shows without opening it.
  const optionsSummary = [
    sortedProfiles.find((p) => p.id === profileIdForChat)?.name,
    effort !== 'auto' ? t(`effort.${effort}.label`) : '',
    !needsRunOn && runMode === 'local' ? t('composer.thisComputer') : '',
    settingsQuery.data?.memory_enabled !== false && memoryOff ? t('memory.off') : '',
  ]
    .filter(Boolean)
    .join(' · ')

  const historyDrawer = (
    <ChatHistoryDrawer
      open={historyOpen}
      mode={historyMode}
      conversations={visibleConversations}
      pinnedIds={pinnedConversationIds}
      selectedId={selectedId}
      loading={conversationsQuery.isLoading}
      error={listError}
      drawerPinned={chatHistoryPinned}
      canPinDrawer={canPinHistory}
      renamingId={renamingId}
      renameValue={renameValue}
      onRenameValueChange={setRenameValue}
      onCommitRename={commitRename}
      onCancelRename={() => setRenamingId(null)}
      onClose={() => setHistoryOpen(false)}
      onNewChat={startNewChat}
      onSelect={(id) => {
        setSelectedId(id)
        if (historyMode === 'overlay') {
          setHistoryOpen(false)
        }
      }}
      onRename={(conversation) => startRename(conversation)}
      onTogglePin={togglePinnedConversation}
      onDelete={handleDelete}
      onDeleteMany={(chats) => {
        setListError(null)
        setPendingDelete(chats)
      }}
      onToggleDrawerPinned={() => {
        const next = !chatHistoryPinned
        setChatHistoryPinned(next)
        if (next && canPinHistory) {
          setHistoryOpen(true)
        }
      }}
    />
  )

  const composer = (
    <form
      className={['composer', showLanding ? 'composer-landing' : ''].filter(Boolean).join(' ')}
      onSubmit={(event) => {
        event.preventDefault()
        void sendMessage()
      }}
      onDragOver={(event) => {
        if (event.dataTransfer.types.includes('Files')) event.preventDefault()
      }}
      onDrop={(event) => {
        if (event.dataTransfer.files.length === 0) return
        event.preventDefault()
        if (!isSending) addFiles(event.dataTransfer.files)
      }}
    >
      {pendingFiles.length > 0 ? (
        <div className="flex flex-wrap gap-1.5 px-3 pt-3">
          {pendingFiles.map((file) => (
            <PendingFileChip key={file.key} file={file} onRemove={() => removePendingFile(file.key)} />
          ))}
        </div>
      ) : null}
      <input
        ref={fileInputRef}
        type="file"
        multiple
        accept={accept}
        className="hidden"
        onChange={(event) => {
          if (event.target.files) addFiles(event.target.files)
          event.target.value = ''
        }}
      />
      {editing ? (
        <div className="flex items-center justify-between gap-2 px-3 pt-2 text-xs text-ink-muted">
          <span>{t('versions.editing')}</span>
          <button type="button" className="text-primary hover:underline" onClick={cancelEdit}>
            {t('versions.cancelEdit')}
          </button>
        </div>
      ) : null}
      <textarea
        ref={composerRef}
        // What is typed takes its own direction; the empty box follows the page.
        dir={draft ? 'auto' : undefined}
        value={draft}
        rows={showLanding ? 3 : 1}
        onChange={(event) => setDraft(event.target.value)}
        onPaste={(event) => {
          if (event.clipboardData.files.length > 0) {
            event.preventDefault()
            addFiles(event.clipboardData.files)
          }
        }}
        onKeyDown={(event) => {
          if (event.key === 'Enter' && !event.shiftKey) {
            event.preventDefault()
            void sendMessage()
          }
          // ↑ in an empty box edits the last message sent (#447).
          if (event.key === 'ArrowUp' && !draft && !editing) {
            const last = [...messages].reverse().find((m) => m.role === 'user' && !m.id.startsWith('optimistic-'))
            if (last) {
              event.preventDefault()
              startEdit(last)
            }
          }
          if (event.key === 'Escape' && editing) {
            event.preventDefault()
            cancelEdit()
          }
        }}
        placeholder={t('composer.placeholder')}
        disabled={isSending}
        className="composer-input"
        aria-label={t('composer.message')}
      />
      <div className="composer-toolbar">
        <div className="flex min-w-0 flex-1 flex-wrap items-center gap-1.5 sm:gap-2">
{accept ? (
          <button
            type="button"
            className="composer-attach"
            onClick={() => fileInputRef.current?.click()}
            disabled={isSending}
            aria-label={t('composer.attach')}
            title={t('composer.attachHint')}
          >
            <svg viewBox="0 0 16 16" className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth={1.5} strokeLinecap="round" aria-hidden>
              <path d="M13.5 7.5 8 13a3.5 3.5 0 0 1-5-5l6-6a2.3 2.3 0 0 1 3.3 3.3L6.4 11.2a1.2 1.2 0 0 1-1.6-1.6L10 4.4" />
            </svg>
          </button>
          ) : null}
          {needsRunOn ? (
            <label className="composer-select ring-1 ring-primary/50" title={runOnTitle}>
              <span className="composer-select-label">{t('composer.runOn')}</span>
              <select
                value={
                  runMode ??
                  (defaultExecution === 'ask' ? '' : effectiveRunMode)
                }
                disabled={isSending || updateConversation.isPending}
                onChange={(event) => chooseRunMode(event.target.value as RunMode)}
              >
                {defaultExecution === 'ask' && runMode == null ? (
                  <option value="" disabled>
                    {t('composer.choose')}
                  </option>
                ) : null}
                <option value="automatic">{t('composer.automatic')}</option>
                <option value="local">{t('composer.thisComputer')}</option>
              </select>
            </label>
          ) : null}
{/* Without choosing models, every chat uses Auto (#589). */}
          {allows('choose_model') ? (
          <label className="composer-select" title={t('composer.modelHint')}>
            <span className="composer-select-label">{t('composer.model')}</span>
            <select
              ref={modelSelectRef}
              value={modelIdForChat ?? ''}
              disabled={isSending || installedModels.length === 0}
              onChange={(event) => setModel(event.target.value)}
            >
              {installedModels.length === 0 ? (
                // "No model installed" only once the list has loaded.
                <option value="">
                  {modelsQuery.isError
                    ? t('composer.modelsFailed')
                    : modelsQuery.isSuccess
                      ? t('composer.noModel')
                      : t('composer.modelsLoading')}
                </option>
              ) : specializedModels.length === 0 ? (
                <>
                  <option value={AUTO_MODEL_ID}>{t('composer.auto')}</option>
                  {installedModels.map((model) => (
                    <option key={model.id} value={model.id}>
                      {model.display_name || model.id}
                      {model.status === 'external' ? t('composer.external') : ''}
                    </option>
                  ))}
                </>
              ) : (
                <>
                  <option value={AUTO_MODEL_ID}>{t('composer.auto')}</option>
                  <optgroup label={t('composer.models')}>
                    {installedModels.map((model) => (
                      <option key={model.id} value={model.id}>
                        {model.display_name || model.id}
                        {model.status === 'external' ? t('composer.external') : ''}
                      </option>
                    ))}
                  </optgroup>
                  <optgroup label={t('composer.specialized')}>
                    {specializedModels.map((model) => (
                      <option key={model.id} value={model.id}>
                        {model.display_name}
                      </option>
                    ))}
                  </optgroup>
                </>
              )}
            </select>
          </label>
          ) : null}
          <div ref={optionsRef} className="relative">
            <button
              ref={optionsButtonRef}
              type="button"
              className="composer-options-button"
              aria-expanded={optionsOpen}
              aria-controls="composer-options"
              title={t('composer.options')}
              onClick={() => setOptionsOpen((open) => !open)}
            >
              <svg viewBox="0 0 16 16" className="h-3.5 w-3.5 shrink-0" fill="none" stroke="currentColor" strokeWidth={1.5} strokeLinecap="round" aria-hidden>
                <path d="M2.5 4.5h7M12.5 4.5h1M2.5 11.5h1M6.5 11.5h7" />
                <circle cx="11" cy="4.5" r="1.5" />
                <circle cx="5" cy="11.5" r="1.5" />
              </svg>
              <span className="sr-only">{t('composer.options')}: </span>
              <span className="truncate">{optionsSummary || t('composer.options')}</span>
            </button>
            {optionsOpen ? (
              <div id="composer-options" role="group" aria-label={t('composer.options')} className="composer-options">
                <label className="composer-select" title={t('composer.profileHint')}>
                  <span className="composer-select-label">{t('composer.profile')}</span>
                  <select
                    ref={profileSelectRef}
                    value={profileIdForChat ?? ''}
                    disabled={isSending || updateConversation.isPending}
                    onChange={(event) => setProfile(event.target.value)}
                  >
                    {sortedProfiles.map((profile: AIProfile) => (
                      <option key={profile.id} value={profile.id}>
                        {profile.name}
                      </option>
                    ))}
                  </select>
                </label>
                {needsRunOn ? null : (
                  <label className="composer-select" title={runOnTitle}>
                    <span className="composer-select-label">{t('composer.runOn')}</span>
                    <select
                      value={
                        runMode ??
                        (defaultExecution === 'ask' ? '' : effectiveRunMode)
                      }
                      disabled={isSending || updateConversation.isPending}
                      onChange={(event) => chooseRunMode(event.target.value as RunMode)}
                    >
                      {defaultExecution === 'ask' && runMode == null ? (
                        <option value="" disabled>
                          {t('composer.choose')}
                        </option>
                      ) : null}
                      <option value="automatic">{t('composer.automatic')}</option>
                      <option value="local">{t('composer.thisComputer')}</option>
                    </select>
                  </label>
                )}
                <label className="composer-select" title={t(`effort.${effort}.hint`)}>
                  <span className="composer-select-label">{t('composer.effort')}</span>
                  <select value={effort} disabled={isSending} onChange={(event) => setEffort(event.target.value as Effort)}>
                    {EFFORTS.map((e) => (
                      <option key={e} value={e} title={t(`effort.${e}.hint`)}>
                        {t(`effort.${e}.label`)}
                      </option>
                    ))}
                  </select>
                </label>
                <MemoryToggle
                  memoryEnabled={settingsQuery.data?.memory_enabled !== false}
                  off={memoryOff}
                  disabled={isSending}
                  onChange={(off) => {
                    if (selectedConversation) {
                      updateConversation.mutate({ id: selectedConversation.id, memory_off: off })
                    } else {
                      setMemoryOffDraft(off)
                    }
                  }}
                />
              </div>
            ) : null}
          </div>
        </div>
        <ContextUsageButton
          usage={contextUsage}
          windowLimit={contextWindow(chatModel?.context)}
          compact={
            selectedConversation
              ? {
                  hasSummary: !!summaryQuery.data,
                  off: !!selectedConversation.compact_off,
                  busy: compactNow.isPending,
                  error: compactError,
                  onView: () => setSummaryOpen(true),
                  onCompactNow: () => compactNow.mutate(selectedConversation.id),
                  onNewChat: () => newChatFromSummary.mutate(selectedConversation.id),
                  onVerbatim: (off) => updateConversation.mutate({ id: selectedConversation.id, compact_off: off }),
                }
              : undefined
          }
        />
        {summaryOpen && summaryQuery.data ? <SummaryDialog summary={summaryQuery.data} onClose={() => setSummaryOpen(false)} /> : null}
        {isSending ? (
          <button type="button" className="composer-stop" onClick={handleStop}>
            <span className="h-2.5 w-2.5 rounded-[3px] bg-current" aria-hidden />
            {t('composer.stop')}
          </button>
        ) : (
          <button
            type="submit"
            className="composer-send"
            disabled={!draft.trim() && !pendingFiles.some((f) => f.status === 'ready')}
            aria-label={t('composer.send')}
            title={t('composer.send')}
          >
            <svg viewBox="0 0 16 16" className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" aria-hidden>
              <path d="M8 13V3.5M3.5 7.5 8 3l4.5 4.5" />
            </svg>
          </button>
        )}
      </div>
      {/* What Auto picked, and what the model can do. A chosen model's name is
          already in the Model menu, so it isn't repeated here. */}
      {isAuto || (chatModel && capabilityLine.length > 0) ? (
        <p
          className="composer-hint"
          title={isAuto ? t('composer.autoHint') : chatToolAssessment?.detail}
        >
          {[
            isAuto
              ? chatModel
                ? t('composer.autoLastUsed', { model: chatModel.display_name || chatModel.id })
                : t('composer.autoPicks')
              : '',
            ...capabilityLine,
          ]
            .filter(Boolean)
            .join(' · ')}
        </p>
      ) : null}
      {!modelIdForChat && modelsQuery.isError && !modelsQuery.data && (
        <p role="alert" className="mt-2 text-xs text-ink-muted">
          {t('composer.modelsFailedHint')}{' '}
          <button
            type="button"
            className="font-medium text-primary underline-offset-2 hover:underline"
            disabled={modelsQuery.isFetching}
            onClick={() => void modelsQuery.refetch()}
          >
            {t('loadError.retry', { ns: 'common' })}
          </button>
        </p>
      )}
      {!modelIdForChat && modelsQuery.isSuccess && (
        <p className="mt-2 text-xs text-ink-muted">
          {downloadBehavior === 'ask' ? t('composer.noModelAsk') : t('composer.noModelDownload')}{' '}
          <Link to="/models" className="font-medium text-primary underline-offset-2 hover:underline">
            {t('composer.openModels')}
          </Link>
        </p>
      )}
      {defaultExecution === 'ask' && !executionAsked && runMode == null && (
        <p className="mt-2 text-xs text-ink-muted">
          <Trans
            t={t}
            i18nKey="composer.chooseRunOn"
            components={{ strong: <span className="font-medium text-ink" /> }}
          />
        </p>
      )}
    </form>
  )

  const suggestions = (
    <div className="mt-5 flex flex-wrap justify-center gap-2">
      <span className="self-center text-xs text-ink-faint">{t('landing.try')}</span>
      {SUGGESTIONS.map((item) => (
        <button
          key={item}
          type="button"
          className="chat-suggestion"
          onClick={() => void sendMessage(t(`landing.suggestions.${item}.prompt`))}
          disabled={isSending}
        >
          {t(`landing.suggestions.${item}.label`)}
        </button>
      ))}
    </div>
  )

  return (
    <div className="page-fill gap-0">
      <div className="flex min-h-0 min-w-0 flex-1 overflow-hidden">
        {showPinnedSidebar ? historyDrawer : null}

        <section className="relative flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
          {/* History on the start side, the chat's name in the middle, and
              its actions at the end, the same at every width. */}
          <div className="chat-header">
            <button
              type="button"
              className={[
                'chat-header-button',
                historyOpen && historyMode === 'overlay' ? 'bg-primary-soft text-primary-active' : '',
              ].join(' ')}
              aria-expanded={historyOpen}
              aria-controls="chat-history-drawer"
              onClick={() => setHistoryOpen((open) => !open)}
            >
              <svg viewBox="0 0 16 16" className="h-4 w-4 shrink-0" fill="none" stroke="currentColor" strokeWidth={1.5} strokeLinecap="round" aria-hidden>
                <path d="M2.5 4h11M2.5 8h11M2.5 12h7" />
              </svg>
              <span className="hidden sm:inline">{t('header.history')}</span>
              <span className="sr-only sm:hidden">{t('header.history')}</span>
            </button>

            <div className="min-w-0 flex-1 px-1 text-center">
              {selectedId ? (
                <>
                  {/* The page's heading while a chat is open; the landing's is hidden then. */}
                  <h1 className="truncate font-display text-[15px] font-semibold leading-tight text-ink">
                    {selectedConversation?.title || t('header.untitledChat')}
                  </h1>
                  <p className="hidden truncate text-[11px] leading-tight text-ink-faint sm:block" title={runOnTitle}>
                    {activeProfile?.name ?? t('header.assistant')}
                    <span className="mx-1.5" aria-hidden>·</span>
                    {effectiveRunMode === 'automatic' ? t('composer.automatic') : t('composer.thisComputer')}
                  </p>
                </>
              ) : null}
            </div>

            {selectedConversation ? (
              <button
                type="button"
                className="chat-header-button hover:bg-danger/10 hover:text-danger"
                title={t('header.deleteChat')}
                aria-label={t('header.deleteChat')}
                onClick={(e) => handleDelete(selectedConversation, e)}
              >
                <svg viewBox="0 0 16 16" className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth={1.4} strokeLinecap="round" strokeLinejoin="round" aria-hidden>
                  <path d="M3 4.5h10M6.5 4.5V3h3v1.5M4.5 4.5l.6 8.5h5.8l.6-8.5M7 7v4M9 7v4" />
                </svg>
              </button>
            ) : null}
            <button
              type="button"
              className="chat-header-button text-ink"
              title={t('header.newChat')}
              onClick={startNewChat}
            >
              <svg viewBox="0 0 16 16" className="h-4 w-4 shrink-0" fill="none" stroke="currentColor" strokeWidth={1.4} strokeLinecap="round" strokeLinejoin="round" aria-hidden>
                <path d="M8.5 2.5H4a1.5 1.5 0 0 0-1.5 1.5v8A1.5 1.5 0 0 0 4 13.5h8a1.5 1.5 0 0 0 1.5-1.5V7.5" />
                <path d="m12 2 2 2-5.5 5.5H6.5v-2z" />
              </svg>
              <span className="hidden sm:inline">{t('header.newChat')}</span>
              <span className="sr-only sm:hidden">{t('header.newChat')}</span>
            </button>
          </div>

          {!showPinnedSidebar && historyMode === 'overlay' ? historyDrawer : null}

          <div className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
            <div
              className={[
                'chat-landing-hero flex flex-col items-center px-4',
                showLanding
                  ? 'flex-1 justify-end pb-2 pt-[min(12vh,5rem)]'
                  : 'chat-landing-hero-exit',
              ].join(' ')}
              aria-hidden={!showLanding}
              inert={!showLanding}
            >
              <Ratatoskr state={landingError ? 'error' : 'idle'} size={96} className="mb-3" />
              <RealmKicker path="/chat" className="justify-center" />
              <h1 className="font-display text-2xl font-semibold tracking-tight text-ink sm:text-3xl">
                {t('landing.title')}
              </h1>
              <p className="mt-2 max-w-md text-center text-sm text-ink-muted">{t('landing.subtitle')}</p>
            </div>

            {selectedId ? (
              <div className="relative min-h-0 flex-1">
                <div
                  ref={scrollerRef}
                  className="chat-transcript absolute inset-0 overflow-y-auto pb-4 [overflow-anchor:none]"
                >
                  <div ref={contentRef} className="chat-column space-y-6 pt-2">
                {messagesQuery.isLoading && <Skeleton label={t('status.loadingChat')} shape="chat" />}
                {messages.map((message) => {
                  const text = displayChatText(message.content)
                  if (message.role === 'assistant' && !text) return null
                  return (
                  <Fragment key={message.id}>
                  <div
                    key={message.id}
                    className={
                      message.role === 'user'
                        ? 'group relative ms-auto flex w-fit max-w-[85%] flex-col items-end'
                        : `group chat-reply${editing === message.id ? ' ring-2 ring-primary/40' : ''}`
                    }
                  >
                    {message.role === 'assistant' ? (
                      <>
                        <ReplyMark />
                        {message.meta?.automation_run ? <AutomationRunNote run={message.meta.automation_run} /> : null}
                        <ChatMarkdown text={text} />
                        <AnswerDetails meta={message.meta} runs={admin} />

                        {message.meta?.setup && admin ? (
                          <SetupOfferCard offer={message.meta.setup} onContinue={continueAfterSetup} />
                        ) : null}
                        {message.meta?.automation && member && allows('automations') ? (
                          <AutomationDraftCard draft={message.meta.automation} conversationId={message.conversation_id} />
                        ) : null}
                        <div className="mt-2 flex flex-wrap items-center gap-2">
                          <VersionSwitch message={message} disabled={isSending} onShow={(id) => void showVersion(id)} />
                          {canReadAloud ? (
                            <ReadAloudButton text={text} conversationId={message.conversation_id} />
                          ) : systemReadAloud ? (
                            <SystemReadAloudButton text={text} />
                          ) : null}
                          {!message.id.startsWith('optimistic-') ? (
                            <AnswerActions
                              disabled={isSending}
                              models={retryModels}
                              onRetry={(modelId) => void sendMessage(undefined, { retryOf: message.id, modelId })}
                            />
                          ) : null}
                        </div>
                      </>
                    ) : (
                      <>
                        <div className={`chat-user-turn max-w-full${editing === message.id ? ' ring-2 ring-primary/40' : ''}`}>
                          <span dir="auto" className="whitespace-pre-wrap">{text}</span>
                          {message.meta?.files?.length ? (
                            <span className="mt-2 flex flex-wrap justify-end gap-1.5">
                              {message.meta.files.map((file) => (
                                <FileChip key={file.id} file={file} />
                              ))}
                            </span>
                          ) : null}
                        </div>
                        {/* Under the bubble, not inside it: with a mouse they show on
                            hover in the gap below, so a hidden Edit leaves no empty
                            band in the bubble; on touch they stay in view. */}
                        {!message.id.startsWith('optimistic-') ? (
                          <span className="mt-1 flex flex-wrap items-center justify-end gap-2 [@media(hover:hover)]:absolute [@media(hover:hover)]:end-0 [@media(hover:hover)]:top-full">
                            <VersionSwitch message={message} disabled={isSending} onShow={(id) => void showVersion(id)} />
                            <EditAction disabled={isSending} onEdit={() => startEdit(message)} />
                          </span>
                        ) : null}
                      </>
                    )}
                  </div>
                  {/* Where auto-compact takes over (#342): the messages above are sent as its summary. */}
                  {summaryQuery.data && message.id === summaryQuery.data.through_message_id && !selectedConversation?.compact_off ? (
                    <div className="flex items-center gap-3 text-xs text-ink-faint" role="separator">
                      <span className="h-px flex-1 bg-line" aria-hidden />
                      <button type="button" className="hover:text-ink hover:underline" onClick={() => setSummaryOpen(true)}>
                        {t('compact.divider')}
                      </button>
                      <span className="h-px flex-1 bg-line" aria-hidden />
                    </div>
                  ) : null}
                  </Fragment>
                  )
                })}

                {isSending && planSteps.length > 0 && (
                  <ol className="chat-aside space-y-1.5 border-s-2 border-norn/40 ps-3" aria-label={t('transcript.plan')}>
                    {planSteps.map((s, i) => (
                      <li key={i} className="flex items-start gap-2 text-sm text-ink-muted">
                        <span aria-hidden className={s.status === 'done' ? 'text-success' : s.status === 'failed' ? 'text-warning' : s.status === 'running' ? 'text-norn' : 'text-ink-faint'}>
                          {s.status === 'done' ? '✓' : s.status === 'failed' ? '!' : s.status === 'running' ? '●' : '○'}
                        </span>
                        <span className={s.status === 'running' ? 'text-ink' : ''}>{s.step}</span>
                      </li>
                    ))}
                  </ol>
                )}

                {teamSteps.length > 0 && (
                  <ol className="chat-aside space-y-1.5 border-s-2 border-primary/30 ps-3">
                    {teamSteps.map((step) => (
                      <li key={step.role} className="text-sm text-ink-muted">
                        <span className="font-medium text-ink">
                          {formatRoleLabel(step.role)}
                        </span>
                        {step.nodeName ? (
                          <span>
                            {' '}
                            <Trans
                              t={t}
                              i18nKey="transcript.onComputer"
                              values={{ computer: step.nodeName }}
                              components={{ computer: <span className="text-info" /> }}
                            />
                          </span>
                        ) : null}
                      </li>
                    ))}
                  </ol>
                )}

                {toolFailure && !isSending && !statusMessage && (
                  <p className="chat-aside text-sm text-ink-muted">
                    {t('tools.failed', { tool: toolFailure })}
                    {' · '}
                    <button
                      type="button"
                      className="text-primary underline-offset-2 hover:underline"
                      onClick={() => void sendMessage(lastUserMessageRef.current)}
                    >
                      {t('tools.retry')}
                    </button>
                  </p>
                )}
                {toolTraces.length > 0 && (
                  <div className="chat-aside text-xs text-ink-muted">
                    <button
                      type="button"
                      className="underline-offset-2 hover:underline"
                      onClick={() => setToolDetailsOpen((open) => !open)}
                    >
                      {t('tools.used', { count: toolTraces.length })} {toolDetailsOpen ? '▾' : '▸'}
                    </button>
                    {toolDetailsOpen && (
                      <ul className="mt-2 space-y-1">
                        {toolTraces.map((trace, index) => (
                          <li key={`${trace.label}-${index}`}>
                            <span className="font-medium text-ink">{trace.label}</span>
                            {trace.detail ? ` — ${trace.detail}` : ''}
                            <span className="text-ink-faint"> · {trace.status}</span>
                          </li>
                        ))}
                      </ul>
                    )}
                  </div>
                )}

                {streamingText ? (
                  <div className="chat-reply">
                    <ReplyMark />
                    <ChatMarkdown text={streamingText} />
                    {responseInterrupted && !isSending ? (
                      <p className="mt-2 text-xs text-ink-muted">{t('transcript.interrupted')}</p>
                    ) : null}
                    {isSending && (
                      <span className="chat-caret ms-0.5 inline-block h-4 w-0.5 bg-primary align-middle" />
                    )}
                  </div>
                ) : null}

                {capabilityNotice && (
                  <CapabilityNotice
                    gap={capabilityNotice}
                    onUse={(modelId) => {
                      setModel(modelId)
                      setCapabilityNotice(null)
                    }}
                    onEnableInternet={admin ? () => void enableInternet() : undefined}
                  />
                )}
                {modelFailure ? (
                  <ModelFailureNotice
                    failure={modelFailure}
                    advanced={advancedMode}
                    onRetry={() => {
                      setModelFailure(null)
                      setResponseInterrupted(false)
                      void sendMessage(lastUserMessageRef.current)
                    }}
                    onChooseModel={() => {
                      ;(profileSelectRef.current ?? modelSelectRef.current)?.focus()
                    }}
                  />
                ) : null}
                {sendError && (
                  <ChatErrorCard
                    raw={sendError}
                    code={sendErrorCode?.text === sendError ? sendErrorCode.code : undefined}
                    mascot={!modelFailure}
                    onRetry={() => {
                      setSendError(null)
                      void sendMessage(lastUserMessageRef.current)
                    }}
                    onNewChat={startNewChat}
                  />
                )}
                {routedModel && routedModel.chatId === selectedId && !isSending && atLeast('admin') ? (
                  <RatingPrompt
                    modelId={routedModel.modelId}
                    modelName={(modelsQuery.data ?? []).find((m) => m.id === routedModel.modelId)?.display_name ?? routedModel.modelId}
                  />
                ) : null}
                <RatingDialogHost />
                  </div>
                </div>
                {showJump ? (
                  <button
                    type="button"
                    className="chat-jump absolute bottom-3 left-1/2 z-10 -translate-x-1/2 rounded-full border border-line/80 bg-surface/95 px-3 py-1.5 text-xs font-medium text-ink-muted shadow-panel backdrop-blur transition hover:text-ink"
                    onClick={jumpToLatest}
                  >
                    <span aria-hidden>↓ </span>
                    {t('header.latest')}
                  </button>
                ) : null}
              </div>
            ) : (
              !showLanding && <div className="min-h-0 flex-1" aria-hidden />
            )}

            <div
              className={[
                'mx-auto w-full max-w-3xl px-2 transition-[margin,padding] duration-300 ease-out sm:px-6',
                showLanding ? 'mb-1 mt-10 pb-2' : 'mt-auto pb-1 pt-2',
              ].join(' ')}
            >
              {replyInProgress ? (
                <div className="mb-3 px-1">
                  <ChatActivity label={statusMessage} mascot={replyMascot} />
                </div>
              ) : null}
              {composer}
              {showLanding ? suggestions : null}
              {!selectedId && capabilityNotice ? (
                <div className="mt-3">
                  <CapabilityNotice
                    gap={capabilityNotice}
                    onUse={(modelId) => {
                      setModel(modelId)
                      setCapabilityNotice(null)
                    }}
                    onEnableInternet={admin ? () => void enableInternet() : undefined}
                  />
                </div>
              ) : null}
              {!selectedId && modelFailure ? (
                <div className="mt-3">
                  <ModelFailureNotice
                    failure={modelFailure}
                    advanced={advancedMode}
                    mascot={!showLanding}
                    onRetry={() => {
                      setModelFailure(null)
                      setResponseInterrupted(false)
                      void sendMessage(lastUserMessageRef.current)
                    }}
                    onChooseModel={() => {
                      ;(profileSelectRef.current ?? modelSelectRef.current)?.focus()
                    }}
                  />
                </div>
              ) : null}
              {!selectedId && sendError ? (
                <div className="mt-3 flex justify-center">
                  <ChatErrorCard
                    raw={sendError}
                    code={sendErrorCode?.text === sendError ? sendErrorCode.code : undefined}
                    mascot={!showLanding && !modelFailure}
                  />
                </div>
              ) : null}
            </div>
          </div>
        </section>
      </div>

      {pendingDelete && (
        <div
          ref={deleteDialogRef}
          className="fixed inset-0 z-50 flex items-end justify-center scrim p-4 sm:items-center"
          role="dialog"
          aria-modal="true"
          aria-labelledby="delete-chat-title"
        >
          <div className="card w-full max-w-sm space-y-4 shadow-panel">
            <div>
              <h2
                id="delete-chat-title"
                className="font-display text-lg font-semibold text-ink"
              >
                {pendingDelete.length === 1 ? t('deleteDialog.title') : t('deleteDialog.titleMany', { count: pendingDelete.length })}
              </h2>
              <p className="mt-1 break-words text-sm text-ink-muted">
                {pendingDelete.length === 1
                  ? t('deleteDialog.body', { title: pendingDelete[0].title || t('deleteDialog.untitled') })
                  : t('deleteDialog.bodyMany')}
              </p>
              {pendingDelete.length > 1 && selectedId && pendingDelete.some((c) => c.id === selectedId) ? (
                <p className="mt-1 text-sm text-ink-muted">{t('deleteDialog.includesOpen')}</p>
              ) : null}
              {automationChats > 0 ? (
                <p className="mt-1 text-sm text-ink-muted">
                  {pendingDelete.length === 1
                    ? t('deleteDialog.automationOne')
                    : t('deleteDialog.automationMany', { count: automationChats })}
                </p>
              ) : null}
            </div>
            <div className="flex flex-wrap justify-end gap-2">
              <button
                type="button"
                className="btn-secondary"
                data-autofocus
                onClick={() => setPendingDelete(null)}
              >
                {t('deleteDialog.cancel')}
              </button>
              <button type="button" className="btn-danger" onClick={confirmDelete}>
                {pendingDelete.length === 1 ? t('deleteDialog.delete') : t('deleteDialog.deleteMany', { count: pendingDelete.length })}
              </button>
            </div>
          </div>
        </div>
      )}

      {undoable && (
        <div className="pointer-events-none fixed inset-x-0 bottom-4 z-40 flex justify-center px-4">
          <div
            role="status"
            className="pointer-events-auto flex items-center gap-3 rounded-xl border border-line/70 bg-surface px-4 py-2.5 text-sm text-ink shadow-panel"
          >
            <span>{t('deleteDialog.deleted', { count: undoable.ids.length })}</span>
            <button type="button" className="font-medium text-primary hover:underline" onClick={undoDelete}>
              {t('deleteDialog.undo')}
            </button>
          </div>
        </div>
      )}

      {pendingTool && (
        <div
          ref={toolDialogRef}
          className="fixed inset-0 z-50 flex items-end justify-center scrim p-4 sm:items-center"
          role="dialog"
          aria-modal="true"
          aria-labelledby="tool-permission-title"
        >
          <div className="card w-full max-w-md space-y-4 shadow-panel">
            <div>
              <h2
                id="tool-permission-title"
                className="font-display text-lg font-semibold text-ink"
              >
                {t('tools.permissionTitle', { tool: toolDisplayName(pendingTool.toolId) })}
              </h2>
              <p className="mt-1 text-sm text-ink-muted">{pendingTool.reason || t('tools.permissionReason')}</p>
            </div>
            <p className="break-words text-sm text-ink">{pendingTool.argsSummary}</p>
            {advancedMode && pendingTool.rawArgs && (
              <pre className="log-panel max-h-40">{pendingTool.rawArgs}</pre>
            )}
            <div className="flex flex-wrap gap-2">
              <button
                type="button"
                className="btn-primary"
                disabled={toolDeciding}
                onClick={() => void decidePendingTool(true, false)}
              >
                {t('tools.allowOnce')}
              </button>
              <button
                type="button"
                className="btn-secondary"
                disabled={toolDeciding}
                onClick={() => void decidePendingTool(true, true)}
              >
                {t('tools.allowSession')}
              </button>
              <button
                type="button"
                className="btn-secondary"
                data-autofocus
                disabled={toolDeciding}
                onClick={() => void decidePendingTool(false)}
              >
                {t('tools.deny')}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
