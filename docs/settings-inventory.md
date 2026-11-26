# Settings and navigation inventory

The source of truth for where every setting lives, as section 10 of [Navigation and settings](navigation-and-settings.md) asks. Audit and structure: #1374. Settings reorganization: #1321.

A check keeps this file complete: `web/src/features/settings/inventory.test.ts` fails when a field of `SettingsPatch` (`web/src/types/api.ts`) has no row here. Add the row in the same pull request as the setting.

Columns:

- **Where today** is the page and card in the web UI before 1.15's reorganization.
- **Category** is the place in the proposed structure below (section 3.2 and 3.3 of the standard).
- **Scope**: Account (each person's own), Application (this computer, or the whole team of computers), Resource (one model, profile, chat, automation, portal).
- **Complexity**: Basic, Advanced, Developer, Administrative (section 4.2).
- **Impact**: Behavioral, Visual, Security, Data, Network.

## 1. Settings stored in settings (`SettingsPatch`)

| Name | Key | Where today | Category | Scope | Complexity | Default | Dependencies | Impact |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Language of the app | `ui_locale` | Settings › General › Language | General | Account | Basic | Device language | none | Visual |
| Language of answers | `assistant_language_mode` | Settings › General › Language | General | Account | Basic | Automatic (the language you write in) | none | Behavioral |
| Answer language (fixed) | `assistant_language` | Settings › General › Language | General | Account | Basic | none | `assistant_language_mode` = language | Behavioral |
| Suggest a better model | `suggest_models` | Settings › Privacy › History | Personalization | Account | Basic | On | none | Behavioral |
| Thinking sound | `thinking_sound` | Settings › Voice | Voice | Account | Basic | On | none | Behavioral |
| Memory | `memory_enabled` | Memory page | Personalization | Account | Basic | On | none | Data |
| Hugging Face token | `huggingface_token` | Models › a model behind a license | Models (contextual) | Account | Advanced | none | none | Security |
| Show community ratings | `community_ratings` | Settings › Privacy › Community ratings | Models (contextual) | Application | Basic | Off | none | Network |
| Quality results | `quality_results` | Settings › Privacy › Community ratings | Models › Results (contextual) | Application | Basic | On | none | Network |
| Ask me to rate models | `ratings_prompts` | Settings › Privacy › Community ratings | Models (contextual) | Application | Basic | On | none | Behavioral |
| Check for updates | `update_check` | Settings › About | About | Application | Basic | On | none | Network |
| AI news | `ai_news` | Settings › Privacy › AI news | News (contextual) | Application | Basic | Off | none | Network |
| Check links in answers | `check_links` | Settings › Privacy › Links in answers | Privacy & security | Application | Advanced | Off | none | Network |
| Problem reports | `problem_reports` | Settings › Privacy › Problem reports | Privacy & security | Application | Basic | Off | none | Data |
| Compact long chats | `compact_at` | Settings › Privacy › History | Chats & history | Application | Advanced | Normal | none | Behavioral |
| Save chat history | `save_chat_history` | Settings › Privacy › History | Chats & history | Application | Basic | On | none | Data |
| Save task history | `save_task_history` | Settings › Privacy › History | Chats & history | Application | Basic | On | none | Data |
| Computer name | `node_name` | Computers › this computer | Network & devices | Application | Basic | Host name | none | Visual |
| Local network access | `lan_api_enabled` | API Access › Local network access | Network & devices | Application | Administrative | Off | none | Security, Network |
| Access from anywhere | `remote_access_enabled` | API Access › Access from anywhere | Network & devices | Application | Administrative | Off | Local network access, subscription or relay | Security, Network |
| Port for access from anywhere | `remote_access_port` | API Access › Access from anywhere | Network & devices | Application | Advanced | 7333 | `remote_access_enabled` | Network |
| Public address | `remote_access_address` | API Access › Access from anywhere | Network & devices | Application | Advanced | Found automatically | `remote_access_enabled` | Network |
| Open the port on the router | `remote_access_port_mapping` | API Access › Access from anywhere | Network & devices | Application | Advanced | On | `remote_access_enabled` | Network |
| Your organization's relay | `remote_access_relay` | API Access › Access from anywhere | Network & devices | Application | Administrative | Toskar's relay | `remote_access_enabled` | Network |
| Relay enrollment secret | `remote_access_relay_secret` | API Access › Access from anywhere | Network & devices | Application | Administrative | none | `remote_access_relay` | Security |
| Find other computers | `discovery_enabled` | Computers › Available to add | Network & devices | Application | Basic | On | none | Network |
| Web UI on the network | `web_ui_enabled` | API Access › Service | Network & devices | Application | Advanced | On | `lan_api_enabled` | Network |
| Answer on Ollama's port | `ollama_port_enabled` | API keys › Ollama's port | Advanced › API keys | Application | Developer | Off | none | Network |
| Advanced mode | `advanced_mode` | Settings › AI behavior › Advanced mode | Advanced | Application | Basic | Off | none | Visual |
| Model lifecycle | `model_lifecycle` | Settings › AI behavior › Model lifecycle | Models & AI | Application | Advanced | Automatic | none | Behavioral |
| Keep models ready for | `idle_unload_minutes` | Settings › AI behavior › Model lifecycle | Models & AI | Application | Advanced | 15 minutes | `model_lifecycle` = automatic | Behavioral |
| Keep running in background | `keep_running_in_background` | Settings › General | General | Application | Basic | Off (on while a schedule exists) | Automations with a schedule | Behavioral |
| Launch at login | `launch_at_login` | Settings › General (desktop app only) | General | Application | Basic | Off | Desktop app | Behavioral |
| Default profile | `default_profile_id` | Settings › AI behavior › Default profile | Models & AI | Application | Basic | General | Profiles | Behavioral |
| Run chats on | `default_execution` | Settings › AI behavior › Run chats on | Models & AI | Application | Advanced | Automatic | Paired computers | Behavioral |
| Download models | `download_behavior` | Settings › AI behavior › Download models | Models & AI | Application | Basic | Ask | none | Data, Network |
| How voice hears you | `voice_hearing` | Settings › Voice | Voice | Application | Basic | Fast | none | Behavioral, Data |
| Language voice hears | `voice_language` | Settings › Voice | Voice | Application | Basic | Detect each turn's language | none | Behavioral |
| Storage limit for models | `model_storage_limit_gb` | Settings › Storage › Storage limit | Models & AI | Application | Advanced | Unlimited | none | Data |
| Notify when a task finishes | `notify_task_finish` | Settings › Notifications | Notifications | Account (#1391) | Basic | On | none | Behavioral |
| Notify when a computer goes offline | `notify_peer_offline` | Settings › Notifications | Notifications | Application | Basic | On | Paired computers | Behavioral |
| Notify about newer models | `notify_newer_models` | Settings › Notifications | Notifications | Application | Basic | On | none | Behavioral |
| Automation digest | `automation_digest` | Settings › Notifications | Notifications | Application | Basic | Off | Automations | Behavioral |
| Digest time zone | `automation_digest_zone` | Settings › Notifications (set from the browser) | Notifications | Application | Basic | Browser's zone | `automation_digest` | Behavioral |
| Terminal commands | `tool_terminal` | Settings › Privacy › Tool permissions | Privacy & security | Application | Advanced | Ask | Profiles can be stricter | Security |
| File changes | `tool_file_writes` | Settings › Privacy › Tool permissions | Privacy & security | Application | Advanced | Ask | Profiles can be stricter | Security |
| Git | `tool_git` | Settings › Privacy › Tool permissions | Privacy & security | Application | Advanced | Ask | Push always asks | Security |

## 2. Settings and controls stored elsewhere

| Name | Where today | Category | Scope | Complexity | Default | Impact |
| --- | --- | --- | --- | --- | --- | --- |
| Theme | Settings › General › Appearance (this browser) | Appearance | Account (per device) | Basic | Dark | Visual |
| Personalization (tone, length, about you) | Settings › General › Personalization | Personalization | Account | Basic | none | Behavioral |
| Use this device's location (#1310) | Settings › General › Personalization (this browser) | Personalization | Account (per device) | Basic | Off | Privacy |
| Your account (name, sign-in, PIN) | Settings, top | Account | Account | Basic | none | Security |
| Your devices (phones signed in as you) | Settings, top | Account | Account | Basic | none | Security |
| Microphone and auto gain for voice mode | Voice mode dialog (this browser) | Voice | Account (per device) | Basic | System default, auto gain on | Behavioral |
| Voice (voices, live voice, Kitten) | Settings › AI behavior › Voice | Voice | Application, with a per-person voice | Basic | Kokoro | Behavioral |
| Notification destinations | Settings › Notifications | Notifications | Application | Basic | none | Network |
| Your data stays private | Settings › Privacy | Privacy & security | Application | Basic | none | none (information) |
| What left this computer | Settings › Privacy | Privacy & security | Application | Basic | none | none (record) |
| Chat cleanup (delete old chats) | Settings › Privacy › History | Chats & history | Application | Basic | Keep | Data, destructive |
| Folders (data, models, runtimes, logs) | Settings › Storage › Folders | Models & AI (data and models), Advanced (runtimes, logs) | Application | Advanced | Platform default | none (information) |
| Version, build, source | Settings › About | About | Application | Basic | none | none |
| Reset application | Settings › Danger zone | Advanced › Danger zone | Application | Administrative | none | Data, destructive |
| API keys | API Access › API keys | Developer | Application | Developer | none | Security |
| Devices connected with a code | API Access › Devices | Network & devices | Application | Administrative | none | Security |
| Use Toskar in other AI apps (MCP) | API Access › Use Toskar in other AI apps | Connections | Application | Advanced | Off | Security, Network |
| External server | Models › External server | Connections | Application | Advanced | none | Network |
| Tools, sources, ComfyUI | Tools | Connections | Application | Advanced | Built-in tools | Behavioral, Security |
| People, roles, sign-in, PINs | People | People & portals | Application | Administrative | Owner only | Security |
| Portals | Portals | People & portals | Application | Administrative | none | Security, Network |
| Computers, deploy a model, join by command | Computers | Network & devices | Application | Administrative | This computer | Network |
| Performance, benchmarks | Performance | Advanced | Application | Developer | none | none |
| Diagnostics, logs, caches | Diagnostics | Advanced | Application | Developer | none | none |
| Per-profile model, tools, swarm | Profiles › a profile | Profile (contextual) | Resource | Basic to Advanced | none | Behavioral |
| Per-project knowledge and instructions | Chat › a project | Project (contextual) | Resource | Basic | none | Behavioral |
| Per-automation schedule, notify, history | Automations › an automation | Automation (contextual) | Resource | Basic | none | Behavioral |
| Per-model start, stop, remove, terms | Models › a model | Model (contextual) | Resource | Basic | none | Data |

## 3. Primary navigation today (web)

The sidebar has 15 destinations in three groups (`web/src/components/layout/Sidebar.tsx`):

- **Use:** Chat, Automations, Knowledge, Memory, News
- **Customize:** Models, Tools, Profiles
- **Administer** (folded): Train, Computers, People, Portals, API Access, Performance, Diagnostics

Settings is reached from the sidebar's footer.

## 4. Phone app today (`toskar-apps/mobile`)

Tabs: Chat, Activity, Models, Computers, Settings. Settings is one list: This iPhone (where chats run), Chat on the phone (web look-up, suggest models, memory), Language, Speed test, Computer (connected computer, encryption, version, forget), Computer settings (Memory, Personalization, Privacy), About, Danger zone. There is no Notifications section (toskar-apps #498).

## 5. Audit against the section 11 checklist

| Checklist item | Web UI today | Phone app today |
| --- | --- | --- |
| Placement reflects how people understand it | No: Ratings, AI news and Links sit under Privacy; Voice and Advanced mode under AI behavior next to Model lifecycle; network settings split over Settings, Computers and API Access (#1244) | Mostly: but Memory appears twice (Chat on the phone and Computer settings) |
| Appropriate existing category | No: "AI behavior", "Storage" and "Danger zone" are groups the standard doesn't name; About holds the update switch and a Diagnostics link | Yes |
| Scope identified | No: per-person cards sit beside Admin-only ones with nothing saying which is which | Partly: "This iPhone" against "Computer" says it |
| Complexity classification | Partly: Advanced mode hides some controls, but Administer pages are all in primary navigation | Yes |
| Platform conventions | Yes for layout; no jump or search in a long page | Grouped list, yes |
| Navigation complexity | No: 15 primary destinations against about 5 to 7 | Yes: 5 tabs |
| Related settings grouped | No (see the first row) | Yes |
| Consistent, familiar labels | No: Norse names in the page kicker and tooltips (#1251); "Computers" in the sidebar, "nodes" in the URL | Yes |
| Advanced doesn't overwhelm basic | No: Run chats on, Model lifecycle, Tool permissions and Compact are on the default page | Yes |
| Accessibility | Yes (labels, focus, keyboard per design.md) | Yes |
| Destructive actions distinguished | Reset: yes, red and confirmed. Chat cleanup: confirmed, but sits among ordinary toggles in History | Reset: yes |
| Room to grow | No: every new feature adds a card to one page | Yes |
| Notifications findable | Yes | No (#498) |

## 6. Structure (signed off 2026-11-25, built in 1.15)

The web UI follows this since #1374's build: the sidebar in `web/src/components/layout/Sidebar.tsx`, Settings' pages in `web/src/features/settings/settingsNav.ts`, and old links in its `movedPages`. Community ratings are on the Models page and the AI news switch on the News page since #1391; Settings › Models & AI and Privacy & security link to them, so Settings' search still finds them. Notifications and Voice are open to Members since #1391: a Member sets their own task notices and thinking sound there, and the rest of each page stays an Admin's.

### 6.1 Primary navigation: 6 destinations

| Destination | Holds | Why |
| --- | --- | --- |
| Chat | Chats and projects | The main workflow, as in ChatGPT and Claude |
| Automations | Automations and their runs | A workflow people return to |
| Knowledge | Knowledge sources | A workflow (add documents, ask about them) |
| Profiles | Profiles (assistants with their own instructions, model and tools) | ChatGPT's GPTs and Claude's projects are primary |
| Models | Installed models, the store, Train your own (as a tab) | "Browse models" is a primary destination in section 2.5 |
| News | AI news | A feed people read |

Settings stays at the foot of the sidebar with the account, as in ChatGPT and Claude. Memory moves into Settings › Personalization, where ChatGPT and Claude keep it. Tools, Computers, People, Portals, API Access, Performance and Diagnostics move into Settings (below). Train becomes a tab of Models.

### 6.2 Settings: a sub-navigation, one page per category

A left rail on wide screens and a grouped list at 390px, with a search over setting names, and the categories in two labelled groups so scope is always visible.

**You** (everyone, each person's own; section 3.3)

| Category | Contents |
| --- | --- |
| Account | Your account, sign-in and PIN, your devices |
| General | Language of the app and of answers |
| Appearance | Theme |
| Personalization | Personalization, Memory, Suggest a better model |
| Notifications | Notification switches, digest, destinations, and on the phone the push switch |
| Voice | Voices, live voice, your voice |

**This computer and team** (Admins and the Owner; section 3.2)

| Category | Contents |
| --- | --- |
| Features | Each feature, Ready or Needs setup, and one Set up that installs what it still needs (#1355) |
| Models & AI | Default profile, Download models, Storage limit, model and data folders; Advanced: Run chats on, Model lifecycle |
| Chats & history | Save chats, save tasks, compact long chats, chat cleanup (destructive, set apart) |
| Network & devices | Computer name, Computers (pair, deploy, join), Find other computers, Local network access, Access from anywhere, Devices connected with a code. This settles #1244 |
| People & portals | People, roles, sign-in, PINs; Portals |
| Connections | Tools and sources, ComfyUI, External server, Use Toskar in other AI apps (MCP), Notification destinations link |
| Privacy & security | Your data stays private, What left this computer, Tool permissions, Links in answers, Problem reports |
| Advanced | Advanced mode, Performance, Diagnostics, runtime and log folders; Developer: API keys, Web UI on the network; Danger zone: Reset |
| About | Version, build, source, Check for updates |

Contextual moves (section 3.4), built: community ratings to the Models page and the AI news switch to the News page (#1391), per-automation history to the automation (#1335).

### 6.3 Names (#1251)

Plain names in every functional place: navigation, page titles, settings, buttons. Norse names stay only as decoration (the rune, the lore page, the About card), never as the label or the page kicker. "Computers" everywhere, and `/nodes` redirects to `/settings/computers`.

### 6.4 Links

Every moved page keeps its old path as a redirect: `/memory`, `/tools`, `/train`, `/nodes`, `/people`, `/portals`, `/api-access`, `/performance`, `/diagnostics`, and the `#` anchors into Settings used by notices, onboarding, the docs and the phone.

### 6.5 Phone app

Settings follows the same two groups as native grouped lists: **You** (Language, Notifications, Voice, Chat on the phone) and **Computer** (connected computer, Memory, Personalization, Privacy), then About and the Danger zone. Memory appears once.
