# Yggdrasil Core — AI Experience Platform

## Feature Specification

### Status

**Type:** Platform Architecture / Product Experience  
**Primary goal:** Build the infrastructure around interchangeable local models so Yggdrasil behaves like one coherent AI system rather than a collection of model runtimes.

**Primary subsystems:**
- **Huginn** — orchestration, routing, planning, verification
- **Muninn** — memory, conversation context, context assembly
- **Mimir** — knowledge retrieval / RAG
- **Gungnir** — tools, capabilities, permissions, providers
- **Norn** — scheduling, placement, execution routing
- **Heimdall** — health, diagnostics, failure recovery
- **Gjallarhorn** — notifications and delivery

---

## 1. Product Goal

Yggdrasil should make a local model feel like a complete AI product.

The model itself is one interchangeable component inside a broader system. A normal user should not need to understand system prompts, context windows, memory retrieval, tool schemas, model routing, structured outputs, retries, hardware placement, providers, artifacts, citations, or connector credentials.

The desired experience is:

```text
Ask Yggdrasil for something
        ↓
gather the right context
        ↓
choose the right model and tools
        ↓
execute on the right hardware
        ↓
verify when useful
        ↓
return a polished answer or artifact
```

> **Local AI should feel like a complete service, not a raw model endpoint.**

---

## 2. Why This Matters

A raw model generates tokens. A strong AI product also provides:

- continuity,
- current information,
- tools,
- memory,
- retrieval,
- planning,
- verification,
- failure recovery,
- background execution,
- notifications,
- integrations,
- artifact handling,
- polished presentation.

Yggdrasil should deliberately own those layers.

Conceptually:

```text
Models
= interchangeable intelligence

Yggdrasil
= context
+ memory
+ retrieval
+ tools
+ permissions
+ orchestration
+ routing
+ recovery
+ scheduling
+ notifications
+ integrations
+ UX
```

---

# Part I — Reference Architecture

## 3. High-Level Architecture

```text
                         USER
                           │
                           ▼
                    Yggdrasil Client
                           │
                           ▼
                        HUGINN
                 request orchestration
                           │
        ┌──────────────────┼──────────────────┐
        ▼                  ▼                  ▼
      MUNINN             GUNGNIR             NORN
  context / memory    tools / permissions   placement
        │                  │                  │
        ├──────► MIMIR ◄───┤                  │
        │       knowledge   │                  │
        └──────────────────┼──────────────────┘
                           ▼
                 Models / Tools / Providers
                           │
                           ▼
                       HEIMDALL
                 health / diagnostics
                           │
                           ▼
                        HUGINN
                 verify / synthesize
                           │
                           ▼
                     GJALLARHORN
                background notifications
                           │
                           ▼
                          USER
```

---

## 4. Subsystem Responsibilities

### Huginn
Owns request-level orchestration:
- task classification,
- model routing,
- reasoning budget,
- planning,
- worker creation,
- multi-step execution,
- verification,
- synthesis,
- fallback coordination.

### Muninn
Owns memory and context:
- conversation history,
- summarization,
- persistent memory,
- context selection,
- context-budget management,
- cross-model continuity.

### Mimir
Owns knowledge retrieval:
- indexing,
- RAG,
- project knowledge,
- semantic retrieval,
- source metadata.

### Gungnir
Owns tools and capabilities:
- tool registry,
- permissions,
- provider selection,
- schema validation,
- connector access,
- invocation,
- tool-result normalization.

### Norn
Owns scheduling and placement:
- node selection,
- model placement,
- tool placement,
- scheduled execution,
- resource-aware routing.

### Heimdall
Owns health and diagnostics:
- model health,
- provider health,
- failure detection,
- degraded nodes,
- timeout/stall signals.

### Gjallarhorn
Owns notifications:
- durable notification records,
- desktop/mobile delivery,
- email/webhooks,
- delivery retries,
- quiet hours,
- deduplication.

---

# Part II — Instruction and Policy Layer

## 5. Request Instruction Stack

Every model request should be assembled from product-controlled layers:

```text
Yggdrasil system policy
+ profile instructions
+ tool-use constraints
+ permission policy
+ relevant context
+ conversation history
+ current user message
```

This should be consistent across runtimes.

Critical policy should be enforced outside the model wherever possible.

Examples:

```text
Model requests email.send
→ Gungnir checks permission

Model requests unavailable tool
→ execution layer rejects it

Model returns invalid structured data
→ validator rejects or repairs it
```

---

# Part III — Context Assembly and Compression

## 6. Context Builder

Muninn should assemble only useful context.

Possible sources:

- recent turns,
- summaries of older turns,
- persistent memories,
- project context,
- relevant files,
- Mimir results,
- tool results,
- active task state.

Do not dump all known context into every prompt.

---

## 7. Context Budget

Each request should explicitly budget context:

```text
available context
   ↓
reserve:
- system instructions
- current request
- response budget
   ↓
allocate remainder:
- recent conversation
- memory
- retrieval
- tool results
```

Prefer relevance over raw recency.

---

## 8. Conversation Compression

When context pressure grows:

- keep full original history in storage,
- summarize older conversation for prompt use,
- preserve decisions, commitments, facts, and unresolved tasks,
- allow the summary to be regenerated from source history,
- avoid repeatedly summarizing summaries where practical.

---

## 9. Provenance

Injected context should retain provenance:

```text
conversation
memory
file
knowledge retrieval
tool result
project context
```

This supports debugging and citations.

---

# Part IV — Persistent Memory

## 10. Persistent Memory

Muninn should support durable memory independent of the model.

Examples:

- user preferences,
- project facts,
- recurring workflows,
- hardware information,
- prior decisions.

Recommended modes:

```text
Memory On
→ conversation + relevant persistent memory

Memory Off
→ conversation only
```

Turning memory off should not delete stored memory.

---

## 11. Explicit and Automatic Memory

Support direct instructions such as:

```text
Remember that this project uses Go.
```

Future automatic extraction should be conservative, editable, removable, deduplicated, and provenance-aware.

---

# Part V — Task Classification and Model Routing

## 12. Task Classification

Huginn should classify requests before deciding how to execute them.

Possible classes:

```text
simple chat
current-information lookup
coding
data analysis
document creation
image/audio task
research
multi-step workflow
external action
scheduled/background task
```

Simple tasks should stay simple.

---

## 13. Automatic Model Routing

Potential roles:

```text
default
fast
reasoning
coding
vision
long-context
planner
worker
reviewer
```

Selection may consider:

- task type,
- installed models,
- capability metadata,
- context limits,
- health,
- hardware fit,
- current load,
- expected speed,
- community model ratings,
- profile preferences.

A single model may fill multiple roles.

---

## 14. Fallback Routing

A bounded fallback sequence may be:

```text
preferred model
    ↓
same model on another node
    ↓
alternate quantization
    ↓
alternate compatible model
    ↓
smaller fallback model
    ↓
user-visible failure
```

---

# Part VI — Adaptive Reasoning

## 15. Reasoning Budget

The system should decide how much orchestration a task deserves.

```text
"What is DNS?"
→ direct model call

"Compare five architectures and produce a decision memo."
→ planning + retrieval + verification
```

Optional user-facing control:

```text
Auto
Fast
Balanced
Thorough
```

Default: **Auto**.

These modes should map to orchestration budgets, not expose internal call counts.

---

# Part VII — Tool Selection, Permissions, and Execution

## 16. Relevant Tool Selection

Gungnir/Huginn should expose or select only relevant tools when practical.

Example:

```text
Spreadsheet request
→ files.read
→ spreadsheet.analyze
→ spreadsheet.create
```

Avoid exposing every tool to every call.

---

## 17. Permission Model

Permissions are enforced outside the model.

Suggested states:

```text
Allow
Ask
Deny
```

Example:

```text
web.search      Allow
files.read      Allow
files.write     Ask
email.send      Ask
shell.execute   Ask
```

The model cannot broaden its own permissions.

---

## 18. Capability-First Tool IDs

Prefer:

```text
image.generate
speech.transcribe
email.search
calendar.create
```

over provider-specific names.

Providers should be interchangeable.

---

## 19. Tool Runtime

Common execution path:

```text
tool request
   ↓
schema validation
   ↓
permission check
   ↓
provider selection
   ↓
Norn placement
   ↓
execute
   ↓
normalize result
```

The runtime should support:

- timeout,
- cancellation,
- progress,
- structured results,
- artifacts,
- normalized errors.

Tools may execute on another Yggdrasil node.

---

# Part VIII — Retrieval and Current Information

## 20. Mimir Knowledge Retrieval

Mimir should retrieve relevant user/project knowledge from:

```text
local files
uploaded documents
project indexes
knowledge bases
connected storage
```

---

## 21. Current Information

Gungnir should expose current-information tools such as:

```text
web search
weather
maps
business lookup
financial data
package tracking
```

Huginn determines when external retrieval is required.

Retrieval ranking should consider relevance, freshness, source quality, and scope.

---

# Part IX — Multi-Step Orchestration

## 22. Planning

Create a plan only when useful.

Example:

```text
Find three NAS drives,
compare price/TB,
create a spreadsheet.
```

Potential plan:

```text
1. Find candidates
2. Gather specs/prices
3. Normalize values
4. Calculate price/TB
5. Compare
6. Create spreadsheet
```

---

## 23. Parallel Workers

Independent work may execute concurrently.

Example:

```text
Research Ollama
Research llama.cpp
Research MLX
Research vLLM
       ↓
    Synthesize
```

Possible worker roles:

```text
researcher
coder
data analyst
critic
writer
```

Workers may use different models, tools, and nodes.

---

# Part X — Verification and Critique

## 24. Verification Layer

Huginn may perform a second pass for:

- calculations,
- source consistency,
- generated code,
- contradictions,
- completeness,
- tool success,
- multi-agent synthesis,
- high-impact actions.

Verification should be selective rather than mandatory for every message.

---

# Part XI — Failure Recovery

## 25. Heimdall Integration

Heimdall should expose health for:

```text
models
nodes
runtimes
tools
providers
```

Huginn/Norn should use these signals to route around unhealthy resources.

Retries should be bounded, error-aware, and observable.

---

## 26. Graceful Degradation

Examples:

```text
multi-agent unavailable
→ single model

preferred provider unavailable
→ alternate provider

large model unavailable
→ smaller compatible model
```

If degradation materially changes expected quality, tell the user.

---

# Part XII — Structured Output Validation

## 27. Structured Results

Automations/tools may need machine-readable data:

```json
{
  "price": 420,
  "currency": "USD"
}
```

Users should not need to see internal schemas.

Validation should include:

- schema checks,
- type checks,
- required fields,
- safe repair/retry.

User-facing rendering:

```text
The current price is $420.
```

---

# Part XIII — Artifact Management

## 28. Unified Artifact Store

Use one abstraction for:

```text
documents
PDFs
spreadsheets
images
audio
video
code files
archives
```

Conceptual metadata:

```text
Artifact
  id
  type
  mime_type
  name
  size
  created_at
  producer
  source_task
  storage_location
```

Support temporary and persistent artifacts, export/download, cross-tool use, and conversation attachments.

---

# Part XIV — Citations and Provenance

## 29. Source Tracking

Retrieved information should retain source metadata.

Possible source types:

```text
web
file
memory
knowledge base
email
calendar
tool result
```

Citation rendering should be a product feature rather than asking the model to invent citation syntax.

---

# Part XV — Scheduling and Notifications

## 30. Background Execution

Scheduled tasks should use the same stack as interactive tasks:

```text
Norn schedules
→ Huginn orchestrates
→ Muninn/Mimir provide context
→ Gungnir runs tools
→ Heimdall watches health
→ Gjallarhorn delivers result
```

Avoid creating a separate scheduler-specific AI stack.

---

## 31. Notifications

Gjallarhorn should support:

```text
in-app
desktop
mobile push
email
webhook
```

Notification policy may be:

```text
always
condition true
result changed
failure
store only
```

---

# Part XVI — Connectors and Authentication

## 32. Connected Services

Examples:

```text
Gmail
Google Calendar
GitHub
Slack
Drive
Notion
Home Assistant
```

Credentials must remain outside model context.

Preferred flow:

```text
model requests capability
      ↓
Gungnir resolves connector
      ↓
connector uses stored credential
      ↓
sanitized result returns
```

Use narrow permission scopes where available.

---

# Part XVII — UI Presentation

## 33. Human-Friendly Activity

Raw execution should be translated into simple UX.

Raw:

```json
{
  "tool": "spreadsheet.analyze",
  "rows": 1482
}
```

User sees:

```text
Analyzing spreadsheet…
✓ 1,482 rows analyzed
```

Normal users should see concise states such as:

```text
Searching…
Analyzing…
Generating…
Verifying…
Creating file…
```

Advanced users can expand technical details.

---

## 34. Error Presentation

Prefer:

```text
I couldn't reach the website. I'll try again on the next scheduled run.
```

over:

```text
ECONNRESET
```

Technical details belong in Diagnostics.

---

# Part XVIII — Observability

## 35. Run Tracing

Every orchestrated request should have a run ID.

Conceptual:

```text
Run
  id
  conversation_id
  profile_id
  strategy
  models_used
  tools_used
  nodes_used
  started_at
  completed_at
  status
```

Advanced run details may show:

```text
Planner: Qwen Reasoning
Workers: 3
Tools: web.search, code.execute
Nodes: MacBook, GPU workstation
Verification: 1 pass
Retries: 0
```

Useful metrics include:

- latency,
- TTFT,
- model load time,
- tokens/sec,
- tool duration,
- retry count,
- context size,
- cache hits.

---

# Part XIX — Caching

## 36. Cache Layer

Potential cache targets:

```text
tool metadata
model capability metadata
retrieval indexes
public model ratings
download metadata
safe external lookups
```

Each cache should define:

```text
key
TTL
invalidation policy
scope
privacy level
```

---

# Part XX — Capability Discovery

## 37. Unified Capability Inventory

Yggdrasil should know:

```text
which models exist
which nodes are online
which tools exist
which providers are healthy
which connectors are authorized
which artifacts are accessible
```

Huginn should be able to ask capability questions without hardcoding providers.

Examples:

```text
Can I generate an image?
Can I execute Python?
Can I access email?
Which node can run this model?
```

---

# Part XXI — Personalization

## 38. Personalization Sources

Personalization may use:

- profile configuration,
- persistent memory,
- response-style preferences,
- preferred tools,
- project context,
- recurring workflows.

Preferences and permissions must remain separate.

Example:

```text
User frequently uses email
```

does **not** imply:

```text
email.send = allowed
```

---

# Part XXII — Default User Experience

## 39. Zero-Configuration Default

Recommended defaults:

```text
Model selection        Auto
Tools                  Auto within permissions
Memory                 On
Context                 Auto
Reasoning               Auto
Planning                Auto
Verification            Auto
Placement               Auto
Fallbacks               Auto
```

Example user request:

```text
Compare the top three options and make me a spreadsheet.
```

Visible:

```text
Researching…
Comparing results…
Creating spreadsheet…
✓ Done
```

Invisible:

```text
classification
model routing
retrieval
parallel work
calculation
artifact generation
verification
```

---

# Part XXIII — Advanced Controls

## 40. Profiles & Orchestration

Advanced users may configure:

- model roles,
- reasoning level,
- planning,
- workers,
- parallelism,
- verification,
- tool permissions,
- providers,
- memory policy,
- context budget,
- node placement,
- retries,
- fallbacks,
- timeouts.

Recommended advanced area:

```text
Profiles & Orchestration
```

Sections:

```text
Profile
Models
Tools
Memory
Orchestration
Execution
```

---

# Part XXIV — Data/API Concepts

## 41. Experience Run

Conceptual:

```text
ExperienceRun
  id
  user_request
  profile_id
  orchestration_strategy
  context_bundle_id
  model_runs[]
  tool_runs[]
  artifacts[]
  citations[]
  status
  started_at
  completed_at
```

---

## 42. Context Bundle

Conceptual:

```text
ContextBundle
  id
  items[]
  token_estimate
  provenance[]
  generated_at
```

---

## 43. Capability Descriptor

Conceptual:

```text
Capability
  id
  type
  provider
  permissions
  requirements
  health
  execution_targets
```

---

# Part XXV — Relationship to Existing Feature Docs

## 44. Umbrella Role

This document coordinates existing feature work rather than replacing it.

Related docs include:

```text
persistent-memory-and-cross-model-context.md
orchestration-layer-refactor.md
expanded-tool-platform.md
scheduler-and-automations.md
gjallarhorn-notification-system.md
community-model-ratings.md
one-line-node-join.md
kubernetes-native-model-deployment.md
train-your-own-ai.md
```

The detailed behavior for those systems should remain in their dedicated feature documents.

---

# Part XXVI — Implementation Plan

## 45. Phase X0 — Inventory Existing Experience Logic

Document current:

- system prompts,
- profile behavior,
- context construction,
- tool exposure,
- model routing,
- error handling,
- artifact handling,
- UI progress.

Goal: identify product logic already spread across Core/Desktop.

---

## 46. Phase X1 — Standard Request Pipeline

Create one pipeline:

```text
request
→ classify
→ build context
→ choose strategy
→ select model/tools
→ execute
→ verify
→ present
```

All interactive chat requests should pass through it.

---

## 47. Phase X2 — Context Service

Consolidate:

- conversation history,
- summaries,
- persistent memory,
- Mimir retrieval,
- tool-result context.

---

## 48. Phase X3 — Routing and Reasoning

Implement:

- task classification,
- automatic model routing,
- adaptive reasoning budget,
- direct vs orchestrated execution.

---

## 49. Phase X4 — Tool/Permission Integration

Integrate the expanded Gungnir platform:

- capability discovery,
- relevant-tool selection,
- permissions,
- schema validation,
- provider routing.

---

## 50. Phase X5 — Verification and Recovery

Integrate:

- selective critique,
- structured validation,
- Heimdall health,
- bounded retries,
- fallback routing.

---

## 51. Phase X6 — Artifact and Provenance Layer

Add:

- unified artifacts,
- provenance metadata,
- citation objects,
- source tracking.

---

## 52. Phase X7 — UX Presentation

Standardize tool/orchestration progress and error rendering.

Add expandable technical details for Advanced mode.

---

## 53. Phase X8 — Background Integration

Ensure scheduled tasks use the same experience stack and Gjallarhorn delivery.

---

## 54. Phase X9 — Personalization and Optimization

Improve:

- memory retrieval,
- preference-aware routing,
- caching,
- community-rating-informed model selection,
- data-driven defaults.

---

# Part XXVII — Acceptance Criteria

## 55. Default Experience

The platform is successful when:

1. A new user gets useful results without configuring orchestration.
2. Relevant conversation context is assembled automatically.
3. Older history can be compressed without deleting source history.
4. Persistent memory survives model changes.
5. Simple and complex requests are treated differently.
6. Models can be routed automatically.
7. Relevant tools can be selected automatically.
8. Permissions are enforced outside the model.
9. Structured outputs are validated before use.
10. Complex tasks can use planning and parallel work.
11. Important outputs can be verified.
12. Model/tool/node failures can trigger bounded fallback.
13. Generated artifacts are handled consistently.
14. Retrieved information retains provenance.
15. Scheduled tasks use the same orchestration stack.
16. Background results can flow through Gjallarhorn.
17. Connector credentials remain isolated from model context.
18. Tool/model activity is rendered in human-readable UI.
19. Advanced users can inspect run details.
20. The user experiences one coherent assistant rather than infrastructure components.

---

## 56. Advanced Experience

Advanced users should be able to inspect or configure:

```text
model routing
role-specific models
tool permissions
context/memory behavior
reasoning budget
planning
parallelism
verification
placement
retries
fallbacks
providers
run traces
```

---

## 57. Reliability

The system should:

- avoid infinite retries,
- avoid repeated OOM loops,
- preserve useful partial results,
- surface meaningful failures,
- separate task failure from notification failure,
- remain functional when optional hosted services are unavailable.

---

# Part XXVIII — Product Outcome

The intended long-term experience is:

```text
User asks Yggdrasil
        ↓
Yggdrasil understands the task
        ↓
recalls relevant context
        ↓
retrieves relevant knowledge
        ↓
chooses the right model
        ↓
chooses the right tools
        ↓
runs work on the right hardware
        ↓
recovers from failures
        ↓
checks the result
        ↓
returns a polished answer/artifact
```

The user should not need to know which model, provider, node, context strategy, or retry path made it possible.

> **Yggdrasil should be the intelligence layer around local models—not merely the program that starts them.**
