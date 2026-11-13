# Swarms

Design note for [#1135](https://github.com/yeixio/toskar-core/issues/1135). It builds on Projects ([#1136](https://github.com/yeixio/toskar-core/issues/1136), [projects.md](projects.md)).

## Goal

A swarm is several agents working at once. A home often has more than one computer, so a swarm can spread across them, and each agent can use a model that suits its part. One engine is used three ways:

1. **Split.** A coordinator breaks one job into parts. Agents do the parts at once, each with its own tools, context and folder, and the coordinator merges and checks the results.
2. **Race.** Several agents, on different models, do the same request. The best result wins, or the results are merged.
3. **Spread.** Concurrent requests from an editor agent's subagents run at once on different computers and models, instead of queueing on one.

## Decisions

These answer the issue's open questions.

1. **Merging code: apply each agent's changes in turn; the coordinator doesn't rewrite them.**
   - Each agent commits to its own branch in its own Git worktree.
   - The coordinator merges the branches one after another into an integration worktree, then runs the checks.
   - A conflict goes to a fix-up agent in the integration worktree, with both sides and both parts' instructions. If that agent fails, the conflict goes to the person.
   - A change rewritten by the coordinator would throw away work each agent has already tested, and on local models it costs a second full pass.
2. **How many agents: from the work, capped by the profile.**
   - The coordinator chooses how many parts the job has, as Team's planner does today. A race uses one agent per model on the profile's list.
   - The profile's `max_workers` (2–8, default 4) is the cap.
   - Free memory is placement's concern, not the planner's: parts that don't fit anywhere wait in a queue, and their lanes say so.
3. **Phone, tablet, TV and car.**
   - The phone and tablet show the lanes read-only: each agent's part, computer, model, status and result, with Stop. They also show the merged answer.
   - The TV, the car and the watch show only the merged answer and a "Working with N agents" line.
   - Approvals are answered on the phone as they are today.
4. **One computer.**
   - llama-server runs one request at a time per model (`--parallel 1`, #834), so a swarm on one computer with one model gains nothing.
   - A swarm runs agents at once only where they really can run at once: on different computers, on different models that are all loaded, or on a model with more than one slot (see Placement).
   - Otherwise it runs as a single agent, and says so in one line: "One agent: this computer can't run more than one at a time with this model."
   - A race on one computer runs only the models that fit together.
   - The quality run measures whether two slots on one model help. Slots stay at 1 until it shows they do.

## Naming

- The new thing is a **swarm**, `swarm_id`. Each agent is an **agent**, and the request that started it is the **coordinator**.
- An agent's role is `agent:N`, alongside the existing `worker:N` and `drafter:N`.
- Team's planner and reviewer keep their roles. The coordinator uses `planner`, and its checks use `reviewer`.
- `project` still means a chat's folder (see [projects.md](projects.md#naming)).

## Data

New migration `074_swarms.sql`:

```sql
CREATE TABLE swarms (
  id              TEXT PRIMARY KEY,
  person_id       TEXT NOT NULL DEFAULT 'owner',
  project_id      TEXT NOT NULL,
  conversation_id TEXT NOT NULL,             -- the coordinator's chat
  mode            TEXT NOT NULL,             -- split | race | spread
  status          TEXT NOT NULL,             -- running | merging | done | stopped | failed
  request         TEXT NOT NULL DEFAULT '',
  budget_json     TEXT NOT NULL DEFAULT '{}',
  result_json     TEXT NOT NULL DEFAULT '{}', -- merge outcome, winner, totals
  created_at      TEXT NOT NULL,
  finished_at     TEXT NOT NULL DEFAULT ''
);
CREATE INDEX swarms_person ON swarms(person_id, created_at);
ALTER TABLE conversations ADD COLUMN swarm_id TEXT NOT NULL DEFAULT '';
ALTER TABLE conversations ADD COLUMN swarm_role TEXT NOT NULL DEFAULT ''; -- '' (coordinator or none) or agent:N
```

- **Each agent's thread is a real chat** in the swarm's project, with `swarm_id` and `swarm_role`. The person can open, read and continue it, and it gets the project's instructions, files and folder.
  - It also means the existing turn machinery works as it is. Only one turn runs per chat (`runs.Swap`), and each agent has its own chat, so agents don't cancel each other.
  - Prior messages come from the agent's own chat. The `contracts.Task` and `Run` signatures don't change.
- **The project:** a swarm runs in its coordinator chat's project. A chat with no project gets one, named after the job, and moves into it. The chat's project line shows this, and the chat can be moved out.
- **History:** `GET /conversations` leaves out agent chats unless `swarm_id=` is given. The history drawer shows only the coordinator. A project page shows agent chats folded under it ("4 agents").
- **Ownership:** a swarm belongs to the person who started it, as chats do.
- **Run records:** each agent's tool calls go into `tool_runs` with that agent's conversation and task, as tasks' calls do today. The swarm row holds the totals.

## The engine

New package `internal/swarm`, called from the orchestrator. A swarm is a strategy, so a profile or a single turn can choose one.

### Coordinator

1. **Plan.**
   - **Split:** the coordinator gets a short, read-only look first: in a task, it can list and read the folder; for a question, it can do a web lookup when allowed.
   - It then writes the parts as JSON, extending Team's plan: each part has a title, instructions, and in a task the paths it expects to change.
   - Parts whose paths overlap are given to one agent, so conflicts are rare by design.
   - **Race:** every agent gets the whole request.
2. **Place.** One agent per part, placed as below.
3. **Run.** Agents start at once, up to what placement allows. The rest queue.
4. **Merge.**
   - **A question:** the agents' answers become reference material for the coordinator's final answer, as `runPlan`'s notes do, but whole rather than as notes.
   - **A task:** branches are merged in turn, as in Decision 1, then the project's checks run (`task_check`). The reviewer reads the combined diff against the request.
   - **Race:** Deliberate's cross-examination and judge (`crossExamine`, `judge`) choose the winner or merge the results. In a task, passing checks counts first: an agent whose checks fail can't win over one whose checks pass.
5. **Deliver.**
   - A question's answer goes in the coordinator chat.
   - A task's merged change is applied to the person's folder as uncommitted changes. Each agent's branch stays, so its work can be looked at alone.
   - Applying the change goes through the same approval that a task's writes do. A change whose checks failed (or timed out) always asks, whatever the profile allows, and the question and the answer's footer say the checks failed.

### An agent

- An agent is an ordinary orchestrator turn (`(*Orchestrator).Run`) in its own chat, with:
  - its own `chatExecEnv`;
  - `turnopts.WithProject(<its worktree>)`;
  - the role `agent:N`, mapped to a model by `profiles.RoleModel`. A race uses the profile's `swarm.models` in order.
- Its first message is the part: the whole request, its part, and what the other agents are doing, so it stays in its lane.
- In a task, an agent gets the task budget (`huginn.TaskBudget`). For a question, it gets the question budget.

### Placement

- The coordinator places agents one at a time with `NodeForRole("agent:N")`, so each sees where the others went (as `plan.go` does). Norn scores by fit as it does now.
- **Concurrency per computer:** each placed model has a number of slots, and agents beyond a model's slots on that computer wait for one to free up.
  - The number is the llama-server `--parallel` value, which stays 1 by default.
  - A new per-model setting, `slots` (1–4), raises it: `PUT /api/v1/models/{id}/slots`, and "Agents at once" in a model's More actions on Models (Advanced mode).
  - llama.cpp splits `--ctx-size` between slots, so each gets `context / slots` (with `--parallel` set, it doesn't use a unified cache). Core starts llama-server with `--ctx-size` set to the window times the slots, so each slot keeps the whole window a chat counts on. The cache takes that many times the memory, and the fit estimate and Auto's window count it.
  - Core refuses a setting that leaves a slot less than the task context, 8K (`MODEL_SLOTS_CONTEXT`), or that doesn't fit the computer, tightly at worst, with every slot's cache (`MODEL_SLOTS_MEMORY`).
  - Changing it reloads the model on its next request, as a context window change does.
  - A paired computer says each model's slots in its models list. A computer from before slots counts as 1.
- **Where things run:** tools always run on the computer Core runs on, where the folder is. Only generation moves to other computers, as it does for Team's workers today.

### Folders

- **A task in a Git folder:**
  - Core first records the person's folder as it is, HEAD plus uncommitted changes and untracked files that aren't ignored, in one snapshot commit made with a separate index, so their index, HEAD, branch and stash are untouched.
  - Each agent gets `git worktree add <data-dir>/swarms/<swarm>/agent-N -b toskar/swarm/<first 8 of swarm>/agent-N <snapshot>`. Starting from the snapshot keeps each branch's diff to the agent's own work, so applying it doesn't collide with what the person already changed.
  - Worktrees are removed when the swarm finishes. Branches are kept until the person deletes the swarm, or for 7 days.
- **A task in a folder that isn't a Git repository:**
  - Copying a whole folder can be expensive and isn't safe to merge, so its parts run one after another in the folder itself.
  - The lanes say why.
- **A question has no folder.** Its agents run at once.

### Budgets

All of these are in `OrchestrationPolicy.Swarm` and can be overridden per turn. When a budget runs out, agents still running are told to stop and report, as a task at its tool-call cap is, and the merge uses what was finished.

| Budget | Default | Range |
| --- | --- | --- |
| Agents | the profile's `max_workers` (4) | 2–8 |
| Tool calls per agent | the task or question budget (100 or the profile's) | as today |
| Tokens for the whole swarm | 400,000 | 10,000–4,000,000 |
| Time | 30 minutes for a task, 5 for a question | 1–240 minutes |

### Approvals

- Each agent's tools ask exactly as a task's do (`Registry.Execute`). A destructive command always asks (`ClassifyCommand`).
- `tool.requested` also carries `swarm_id` and `agent` (for example "Agent 2: tests").
- `Registry.Waiting` can list a whole swarm, so one place shows every question. Clients group them by agent.
- **Allow for this session** applies to the agent's own chat, never to the whole swarm. One agent's approval doesn't widen another's.
- With nobody to ask, a call fails with `ErrNeedsApproval`, as it does today.

### Events

These are on the event stream with `swarm_id`:

| Event | Payload |
| --- | --- |
| `swarm.started` | mode, and agents: `agent`, `conversation_id`, `part`, `role`, `model`, `node_id`, `status` (running or queued) |
| `swarm.agent.started` | agent, node_id, model |
| `swarm.agent.progress` | agent, step, tool, summary (one per tool call, from the agent's tool events) |
| `swarm.agent.finished` | agent, status (done, failed, stopped, out of budget), result summary, tokens, seconds |
| `swarm.merging` | what is being merged, and a conflict when there is one |
| `swarm.finished` | status, winner (race), totals |

- **Stopping one agent:** the existing `POST /api/v1/chat/stop` with that agent's `conversation_id`. The merge goes on without it.
- **Stopping the whole swarm:** stop on the coordinator's chat, or `POST /api/v1/swarms/{id}/stop`.

## Spread: editor agents' subagents

- Editor agents send their own tool definitions and run their tools themselves, so a swarm can't take over their work. What helps them is running their concurrent requests at once. Model names on the three raw formats:
  - **`swarm:<profile>` with client tools** (any request with `tools`): each request goes to the least busy of the profile's swarm models and computers that fits. The body is forwarded as it is, and the answer comes back in the request's format. This is Spread.
    - It needs one new piece: forwarding a raw body to another paired computer's runtime, over the same authenticated link that `Generate` uses.
    - `raw:` keeps its meaning: this computer, one model.
  - **`swarm:<profile>` without client tools:** a full swarm run (Split, or Race when the profile says so). It returns the coordinator's answer, and streams progress as reasoning text where the format has it.
- **`POST /api/v1/swarms`** is for apps that want the lanes. It takes `{prompt, mode, project_id?, conversation_id?, budget?}` and returns the swarm with its agents. `GET /api/v1/swarms/{id}` returns the current state.
- **MCP:** a `run_swarm` tool in `/mcp` (`internal/mcp/server.go`), so `toskarctl mcp` has it too. It takes `{prompt, mode?, profile?}` and returns the answer and a link to the coordinator chat.
- **`toskarctl connect`** gains `--swarm <profile>`, which sets the agent's subagent model to `swarm:<profile>`: Claude Code's `CLAUDE_CODE_SUBAGENT_MODEL`. Its fast model stays the agent's, since its background requests have no tools and would each run a whole swarm.

## Chat and tasks

- **Strategy:** `"swarm"` joins single, planned and team. `OrchestrationPolicy.Swarm` holds `mode` (split or race), `models` (for race), and the budgets.
  - Auto never chooses a swarm until the quality run shows it helps.
- **Asking for one per turn:** `POST /chat` takes `swarm: "split" | "race"` for one turn. The web and desktop composer gets "Use a swarm" in its menu, and the terminal gets `--swarm` and `/swarm`.
  - A message that says "use a swarm" or "use N agents" turns it on for that turn, in English, matched as `goOnRe` is.
- **Lanes** (web and desktop): each agent's lane shows its part, computer, model, its latest steps, and Stop. A lane opens to the agent's chat. The merged answer comes last.
- **`toskarctl chat`:** one line per agent, updated in place, then the answer. `/agents` lists them, and `/open N` continues an agent's chat.
- **Who can use it:**
  - Members and up can start one.
  - A task in a folder needs an Admin, as a folder does today.
  - Phone keys can start a question swarm and read lanes.
  - Portals' guests can't start one.

## API (contract 1.88)

| Method and path | What it does | Role |
| --- | --- | --- |
| `POST /api/v1/swarms` | Start one | Member and up (folder: Admin) |
| `GET /api/v1/swarms/{id}` | One swarm, with agents and status | Its owner |
| `GET /api/v1/swarms?project_id=` | A project's swarms | Its owner |
| `POST /api/v1/swarms/{id}/stop` | Stop all its agents | Its owner |
| `DELETE /api/v1/swarms/{id}` | Delete the record and its kept branches. Its chats stay | Its owner |

- `Conversation` also gets `swarm_id` and `swarm_role`. `GET /conversations` takes `swarm_id=`.
- `POST /chat` gains `swarm`. The profile gains `orchestration.swarm`.
- The raw formats accept `swarm:<profile>`, and `/v1/models` lists them for profiles whose strategy is swarm.
- Updated together: `api/openapi.yaml`, `docs/api.md`, the contract test data, the SDKs (toskar-sdks sync), and tested curl examples.

## Order of work

1. **Core engine:**
   - migration, `internal/swarm`, coordinator and agents as chats, placement with queueing, Git worktrees and merging, budgets, approvals with `swarm_id`, events, stop;
   - the API, contract 1.88, and unit and API tests.
2. **`slots`:** the model setting, and placement counting slots.
3. **Web and desktop:** strategy, "Use a swarm", lanes, approvals grouped by agent, and the project page's folded agents.
4. **Terminal:** `--swarm`, `/swarm`, lanes, `/agents`.
5. **Editors:**
   - `swarm:<profile>` on the raw formats, the raw forward to paired computers, and the concurrency test;
   - MCP `run_swarm`, `toskarctl connect`, and VS Code's `@toskar /swarm`.
6. **Phone and other apps:** a toskar-apps issue for read-only lanes and the merged answer.
7. **Quality run:** a swarm set with long tasks and multi-part questions, comparing a swarm with a single agent on the same models, on the rig and on two computers.
   - It counts tasks done, time and tokens, and tests `slots` 1 against 2.
   - The results decide Auto and the defaults.
8. **User guide page** with screenshots and tested examples, and an integration guide for editor agents.

## Tests that show it works

- **Split:** in a Git folder, two agents edit different files at once, both branches merge, the checks pass, and the person's folder gets one uncommitted change. Two agents editing the same lines produce a conflict that goes to the fix-up agent, then to the person.
- **Running at once:** agents on two computers, or on two slots, overlap in time. Two agents on one model with one slot don't overlap, and the lane says it's queued.
- **One computer, one model, one slot:** a swarm runs as one agent and says why.
- **Approvals:** each agent's approval carries its `swarm_id` and agent. "Allow for this session" in one agent doesn't let another agent's call through. A destructive command asks even when the profile allows the tool.
- **Budgets:** a swarm at its token or time budget stops its agents, and the merge uses what was finished.
- **Stopping:** stopping one agent lets the others finish. Stopping the swarm stops all of them, and any approval they were waiting on is dropped.
- **Ownership:** another person can't see, stop, or open the chats of someone else's swarm.
- **Spread:** two concurrent `swarm:<profile>` requests with client tools run on two computers at once and come back in their own formats.
- **Race:** in a task, an agent whose checks fail can't win over one whose checks pass.
