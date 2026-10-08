# Changelog

All notable changes to Yggdrasil Core are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/). This project uses Git tags. Version numbers below are tags and notes that already exist in the repository. This file does not restate every historical fix.

## [Unreleased]

Changes waiting for the next release are in [changes/unreleased/](changes/unreleased/), one file per pull request. `scripts/changelog.py preview` shows them together, and `scripts/changelog.py release <version>` writes them here when the release is cut.

## [1.7.0] - 2026-10-08

Toskar 1.7 is for more than one person and more than one device. People sign in with their own accounts, by password, invite link, an OpenID Connect provider, or a reverse proxy, and roles decide what each can do; chats, memories, files, and automations stay private to their person, and the existing user becomes the Owner. Automations run on schedules, file and folder changes, web pages, feeds, and webhooks, can chain into each other, and can be set up by asking in a chat. Chat can see pictures and watch videos. Traffic between computers and from phones is encrypted, a phone, tablet, or TV connects with a 6-digit code, and every model shows whether it runs on the GPU. The desktop app's menus and tray call it Toskar Pro, its new name. The API stays compatible: the client contract is 1.15, whose new versions only add fields, and a paired computer still on 1.6 keeps working over plain HTTP, marked Not encrypted, until it's updated. Binaries and the apt repository are not signed.

### Added

- Chain automations: one can run right after another finishes, given its
  result, such as a research automation followed by one that drafts a
  note from it, or only when the first one notifies, such as acting once
  a price drops. Choose "After another automation" under "Runs", or
  `toskarctl automations create --trigger after --after <id>`. Loops are
  refused when saved, and a chain stops after five in a row.
- Continue in chat from any automation run: the result opens in a chat
  named after the automation, or in the chat it was already posted to, so
  you can ask a follow-up about it.
- A daily automation digest: turn it on in Settings → Notifications and
  pick a time, and once a day one message in an "Automation digest" chat
  sums up what every automation found and what failed, with one
  notification instead of many. A quiet day sends nothing.
- Automations can run when files in a folder in your home folder, or one
  file, change. The task is told which files were added, changed, and
  removed, with the start of each new or changed text file. Choose "When a
  file or folder changes" under "Runs", or `toskarctl automations create
  --trigger folder --trigger-path ~/Documents/Invoices`.
- Ask in chat for something on a schedule, such as "every morning at 8,
  summarize the news", and the answer shows the automation as a card:
  what it does, when, and when it notifies. Nothing is scheduled until you
  press Create, and the automation remembers the chat it came from. The
  request is read in the language it's written in.
- Profiles have an Automations capability, on in the built-in profiles,
  for setting up automations from chat.
- `POST /api/v1/automations/parse` reads a request such as "every morning at
  8, tell me if the price is below $500" into an automation's name, task,
  schedule, and notification, in any of the App's languages or English. It
  uses the same words as the Automations page, now kept in
  `i18n/requests/`, so the page, `toskarctl`, and chat can read requests the
  same way.
- `toskarctl automations parse "<request>"` prints what Toskar understood
  from a request, and `toskarctl automations create --request "<request>"`
  creates the automation from it.
- An automation made from a chat posts each result it notifies about to
  that chat, so you can reply to it there. The notification opens the
  chat, and the result is marked with the automation it came from.
- Automations can also save each result as a Markdown file in a folder
  you choose in your home folder, such as `~/Documents/Toskar`, where
  other apps and backups find it. The run history shows where each result
  went. `toskarctl automations create|update --save-folder` sets it.
- Automation templates: morning briefing, price watch, back in stock,
  release notes, page watch, and a new folder summary. Choosing one asks
  only for what it needs, such as a product link and a price, and fills in
  the form without reading a request; "Describe it in your own words
  instead" starts from an example request.
- Automations can run when a web page changes or a feed has new posts,
  instead of every time. The schedule says how often to check; a check is
  a quick fetch with no model, and the task runs only when something
  changed, told what it was: the lines that came and went, or the new
  posts. Choose it under "Runs" in the form, or with `toskarctl
  automations create --trigger page|feed --trigger-url <url>`.
- Automations can run when another service calls their webhook link: pick
  "When another service calls its link (webhook)" under "Runs", then make
  the link on the automation's page. It's shown once, only a hash of it is
  kept, and making a new one stops the old one. The request's body, up to
  64 KB, is given to the task as data. `toskarctl automations hook <id>`
  makes a link from the command line.
- A "Only when started" schedule, for an automation that runs only from
  Run now or its webhook.
- Chat can see pictures. Attach a photo, screenshot, or diagram and ask
  about it: Toskar shows it to a vision model on this computer. If the
  chat's model reads text only, the largest vision model that fits answers
  instead, and a follow-up question still sees the picture. With no vision
  model installed, the answer says how to get one.
- Two vision models in the catalog: **Gemma 3 4B** and **Qwen 2.5 VL 7B**.
  Each downloads with its image projector.
- Chat can watch videos. Attach an MP4, MOV, or WebM clip and ask about it:
  a vision model is shown six frames sampled through it, with the time of
  each, and can transcribe what's said. Frames are read on this computer
  with the speech tools, which install the first time they're needed. A
  follow-up question about the same clip doesn't read it again.
- A phone can connect with a 6-digit code instead of a copied API key: the
  computer shows the code (`POST /api/v1/devices/pairing`), and the phone
  sends it from the local network (`POST /api/v1/devices/pair`) to get a key
  of its own, named after it. A phone's key reaches only chat, its
  conversations, and the lists the phone reads.
- Connect a device, on Computers and in API Access: it shows a 6-digit code
  to type on a phone, tablet, or TV, with the steps and this
  computer's address, turns on local network access first if it's off, and
  says when the device has connected. Connected devices are listed under
  API Access → Devices, where each can be disconnected.
- Settings → Your data stays private says whether this computer's disk is
  encrypted: "Your disk is encrypted with FileVault", or, when it isn't,
  where to turn on FileVault, BitLocker or Device Encryption, or LUKS. It
  checks the disk that holds Toskar's data, without administrator rights.
- Running models report where they run, read from llama.cpp's own log when it loads them: on the GPU, partly, or on the CPU, with the device, the layers on the GPU, the GPU memory, and the reason when a model isn't fully on the GPU. `GET /api/v1/models/running` has it in `acceleration`, and `GET /api/v1/health` sums it up in `acceleration` without changing `status` (#317).
- Performance › Activity marks each reply GPU or CPU, with the device in its details, and filters by it; benchmark results say where each model ran. The user guide has a section, "Is my model using the GPU?", which the assistant also answers from (#317).
- The Linux packages recommend the Vulkan loader, Mesa's Vulkan drivers, `vulkan-tools`, and `pciutils`, so apt and dnf install what an AMD or Intel GPU needs, and the service user joins the `render` and `video` groups so it can use the card. `install.sh` prints the command to install NVIDIA's driver when it finds an NVIDIA card without it, and `install.ps1` points to the card maker's driver when Windows has no Vulkan (#317).
- The Dockerfile has a `gpu` target with Vulkan and Mesa's drivers, for a container given the card with `--device /dev/dri`.
- [docs/gpu.md](docs/gpu.md) covers what each computer needs for its GPU, what the installers set up, and how to fix a model that runs on the CPU.
- `GET /api/v1/performance/live`: each computer's CPU, memory, and GPU figures (busy %, video memory, temperature, power) now, over the last hour, and as per-minute averages over the last day, on Linux, macOS, and Windows without root or admin rights. A figure a computer can't give is left out rather than shown as 0. Paired computers' figures are included (#317).
- Diagnostics lists what this computer still needs for Toskar to use its graphics card, each with the command that fixes it here: the Vulkan loader or drivers (for this Linux distribution's package manager), permission for Toskar's user to open the card, NVIDIA's driver, a Windows card's driver, or the CPU-only llama.cpp. `GET /api/v1/diagnostics/gpu` returns the same list (#317).
- A status on each running model says where it runs: green on the GPU, amber when only part of it fits, red when the computer has a GPU it isn't using, and grey on a computer without one. Tap it for the device, the layers on the GPU, and the GPU memory, and for anything short of green, why and what to do. It's on Models › Running and on the Performance overview (#317).
- The Performance overview shows each computer's live CPU and memory, and each GPU's use, video memory, temperature, and power where the computer gives them, with the last hour as a line. Diagnostics has a GPU acceleration row.
- Replies, run traces, and benchmark samples record the backend and device that produced them, such as Vulkan on an AMD Radeon RX 7900 XTX, so performance history can tell GPU replies from CPU ones. `GET /api/v1/runtimes` lists what the installed llama.cpp build can run on (#317).
- Vision models installed from Hugging Face can see pictures. Browse all
  models marks a repository that ships an image projector as a vision
  model. Installing it downloads the projector with it, and the model's
  download is checked against the SHA-256 the repository lists. Reinstalling
  a vision model installed from Hugging Face before now adds its projector.
- Image and video generation say up front what a computer can do. A
  computer without the memory for a model isn't offered it, can't set it
  up, and is told how much it needs; one that's short of what the model is
  comfortable with is warned it may be slow or fail. Without GPU
  acceleration (every build but macOS), the setup offer, the Tools page,
  and the progress line say a picture takes a few minutes and a clip can
  take most of an hour.
- A saved answer's metadata records the backend and device that ran it, when it ran on that computer, so the iPhone app can show GPU or CPU for answers from a paired computer too. The client contract is 1.8 (#317).
- Everyone can connect their own phone, tablet, or TV from Settings → Your
  devices, and see and disconnect the devices connected as them. A device
  chats as the person who connected it. Only Admins and the Owner turn on
  local network access to do it.
- The start of people and roles: this Toskar's one user becomes its Owner,
  and API keys, chats, memories, files, and automations record whose they
  are. `GET /api/v1/me` says who a request is from. Nothing changes for one
  person; signing in other people comes next.
- Notifications belong to their person. Members see their own automation
  results and approval requests in the bell, and nobody else does; Admins
  and the Owner also see notices about models, training, and health.
  Email, push, webhooks, and desktop notices still go only to the Owner's
  destinations, so they never carry another person's results.
- Sign in with an OpenID Connect provider such as Google, Microsoft Entra
  ID, Okta, Authentik, Keycloak, or Authelia. Set `TOSKAR_OIDC_ISSUER`,
  `TOSKAR_OIDC_CLIENT_ID`, and `TOSKAR_OIDC_CLIENT_SECRET`, and the sign-in
  screen offers it. People are made the first time they sign in, with a
  role from their groups.
- Each person chooses their own App language, the language answers are
  written in, and Personalization, starting from the Owner's. Members and
  Visitors find them in Settings, and their chats and automations follow
  their own choices.
- Chats, memories, files, and automations are private to their person,
  ready for more than one person to use Toskar: nobody else lists, reads,
  or changes them, a chat draws only on its person's memories, and an
  automation runs as its person. Nothing changes while you're the only one.
- Sign in through a reverse proxy such as Authelia, Authentik, Cloudflare
  Access, or Tailscale. List the proxy in `trusted_proxies`
  (`TOSKAR_TRUSTED_PROXIES`), and Toskar takes the person from its
  `Remote-User` header, makes them the first time, and gives them a role
  from their groups. The headers count only from the listed addresses.
- Roles now decide what each person can do. Visitors chat; Members also
  have their own memories and automations; Admins run everything else,
  such as models, tools, knowledge, computers, keys, people, and settings;
  and only the Owner can erase everything. The app shows each person only
  the pages and controls their role can use, and the service refuses the
  rest.
- People can sign in. Admins and the Owner add someone and get a one-time
  link; opening it, they choose a username and password and are signed in.
  Passwords are hashed with argon2id, and too many wrong ones make that
  username wait. Admins add Members and Visitors, the Owner adds Admins
  too, and disabling someone signs them out everywhere. A phone paired with
  a code now gets the key of whoever showed the code. The app's sign-in
  screen and People page come next.
- The app has a People page under Administer. Admins and the Owner add
  someone, pick a role, and copy a one-time link to send them; the page
  also changes roles, makes new password links, and disables people.
  Opening the link, the person chooses a username and password and is
  signed in. From another device, the app now asks for a username and
  password, or an API key, and Settings shows who is signed in with a
  Sign out button.
- Connect a device shows the code of this computer's certificate. A phone
  on the new app connects over HTTPS, saves that certificate, and shows the
  same code in its Settings, so you can see it reached this computer.
- The automation form sets up the new schedules: pick days of the week
  (such as Monday to Friday), add more times a day, run every month on a
  day of the month, or enter a cron expression. Schedules read as people
  say them, such as "Weekdays at 9:00 AM" or "Every Monday and Friday at
  8:00 AM and 5:00 PM", in every App language.
- Automations can run on several weekdays, such as weekdays only, at
  several times a day, monthly on a day of the month (a shorter month uses
  its last day), or on a cron expression. The API takes them as
  `weekdays`, `times`, `month_day`, and `cron`, and so does `toskarctl
  automations create --schedule monthly --day 1` or `--weekday weekdays
  --at 08:00,17:00` or `--schedule cron --cron "0 9 * * 1-5"`. The model
  that reads requests the words can't can now give these schedules too.
- Every page in the sidebar has a screenshot, and the README shows them. Train, Knowledge, Memory, Tools, and Profiles & Orchestration now have demo data in the screenshot server (a training run in progress, connected documents, memories, a Notion tool source with connected services, and a team profile across two computers), so the release attaches `screenshot-train.png` through `screenshot-profiles.png` next to the others (#304). The README stills and walkthrough show the new sidebar.
- Image and video generation use the GPU on Linux and Windows. With a
  graphics card that has a Vulkan driver (NVIDIA, AMD, or Intel), setup
  installs stable-diffusion.cpp's Vulkan build, so a picture takes seconds
  instead of minutes. A Vulkan build that can't start on the GPU falls back
  to the CPU build by itself. The Tools page says which build makes them,
  with a button to switch. An existing setup on the CPU build offers **Use
  the GPU build** there.
- Paired computers that make pictures on the GPU are now reported as
  accelerated, so heavy image work goes to them.
- A chat can offer to set up image or video generation on a paired
  computer. When a laptop would make pictures slowly on its CPU and a
  paired workstation has a GPU for it, "Make me a picture of…" offers the
  workstation, with its free disk space. **Set up** starts the download
  there and shows its progress, and once it's ready the picture is made
  there. A computer without the memory or the disk space isn't offered.
- `site/highlights.json` holds the latest minor release's highlights, which toskar.ai shows as "New in 1.6" instead of a list written into the site. A new minor release updates it in the same pull request: `scripts/changelog.py check` fails until its version matches the latest release in `CHANGELOG.md`.
- Toskar says when a newer version is out. Release builds installed from a
  download look at toskar.ai once a day, sending nothing but the request,
  and Settings › About shows the new version with what's new and a download
  link. Turn it off in About; each check is listed in What left this
  computer. The App Store edition and the desktop app's copy update with
  their app, so they don't check.

### Changed

- API Access, Performance, and Diagnostics match the rest of the app: the
  app picker under Use Toskar in other AI apps and Performance's views are
  segmented tabs, buttons share the same sizes, and Revoke on an API key is
  red, as a destructive action. Diagnostics is titled Diagnostics, as in the
  sidebar.
- The Automations page reads a request on the computer, so the page,
  `toskarctl`, and chat read requests the same way. When its words can't
  find a schedule, such as "first thing on weekdays", a model reads the
  request, and the form says so, so you can check it before saving.
- The computer adds what an automation's condition needs, such as the price
  in the threshold's currency, when each run starts, so automations made
  with `toskarctl`, chat, or the API check their conditions the same way as
  ones made on the page. A saved prompt is only the task.
- A run's history says why it did or didn't notify as the computer decided
  it, instead of the page guessing from the result. Runs record it as
  `notify_detail` and `notify_values`.
- Automations explains itself. With none yet, the page shows the three parts
  of an automation (when it runs, what Toskar does, and when you hear about
  it) and five ideas to start from: a morning news brief, a price watch, a
  back-in-stock check, weekly release notes, and a page watch. Choosing an
  idea opens the form already filled in. With some automations, How it works
  shows the same.
- The new-automation form has two numbered steps, Describe it and Check the
  details, and the button that reads your description is now Fill in the
  details. A Summary above Create automation says when it runs, what it does,
  and when you hear about it. Test run says that it runs once without saving.
- The Automations list uses the same filters, search box, and status chips
  as the rest of the app.
- Chat has a cleaner look. Messages and the message box share one centred
  column; your messages sit in a soft bubble and replies read as plain text
  beside the Toskar mark. The header keeps History, the chat's name, Delete,
  and New chat in one row, and the message box starts small and grows as you
  type.
- The chat history panel matches the new Chat look: icon buttons to dock
  and close it, a search box with a search icon, one line per chat, and
  tidier menus.
- The context gauge beside Send is an open meter that fills as the chat uses
  the model's memory and turns amber near the limit, so it no longer looks
  like an empty radio button.
- The Computers page is titled Computers, as in the sidebar. While this is
  the only computer, it explains how to add another in three steps: install
  Toskar there, connect them (Available to add on the same network, or Add
  by command for a server), and share the work. How to add a computer shows
  the same later. Computer cards use quieter Manage buttons and a menu like
  the rest of the app.
- The desktop app can post desktop notices itself: with
  `TOSKAR_DESKTOP_NOTIFICATIONS=shell`, which the app sets on the service it
  starts, notices go to it as `notification.desktop` events, so they carry
  Toskar's name and icon, open the app when clicked, and work in the Mac App
  Store edition. Without it the service posts them as before.
- The same card now says that traffic between your computers, and from
  other devices on your network, is encrypted.
- Knowledge explains itself and shows where each source is used. Every
  source says which profiles' chats use it; one that no profile uses says
  that Chat doesn't look in it, with Use with to add it to the profile chats
  start with. Connecting a source asks which profiles should use it, with
  that profile already checked, so it works in Chat straight away. With
  nothing connected yet, the page explains knowledge in three steps and shows
  what people connect; How it works shows the same later.
- Turning local network access on or off applies at once, without quitting
  and reopening Toskar. Turning it off also disconnects other devices right
  away.
- Turning on local network access no longer asks apps on this computer for an API key. Requests over loopback (`127.0.0.1` or `::1`) work as before, so the desktop app, `toskarctl`, and a browser on this computer keep working. Other devices still need a key. A request carrying proxy forwarding headers (`Forwarded`, `X-Forwarded-For`, `X-Real-IP`) counts as coming from the network.
- Memory explains itself. With nothing remembered yet, the page shows how
  memory works in three steps (tell it once, it's used when it fits, you stay
  in control), says that documents and data belong in Knowledge, and offers
  example memories that fill in the Add box. How it works shows the same
  later. Use memory in chats is now a switch.
- Each memory shows its words, where it came from, and Paused or This
  computer only when they apply. Edit changes the words and the category;
  Pause, This computer only, and Delete are in the memory's menu.
- Models has a cleaner look. Discover, Installed, and Running are one
  segmented control beside a search box with a search icon. Each model's fit
  shows as a coloured chip (green when it runs well, amber when it's tight,
  red when it won't), and every action in a card is the same size. The
  "Models for" strip is tidier, and on Running, Stop sits beside Open chat
  with a bar for how much of the computer's memory the model holds.
- Local network access no longer needs an API key once someone besides
  the Owner has been added, since they sign in with a password.
- The profile editor opens across the whole page and is split into four
  tabs: Models, Tools, Knowledge and memory, and Strategy and computers. It
  was one long column inside a half-width card. Models shows the roles in
  use, with Show all roles for the rest; Tools uses switches, with per-tool
  permissions folded below them.
- The real-model quality run rides out a daemon that goes away for a moment (a restart or a settings change), retrying for up to three minutes. If the daemon starts asking for an API key partway through, the run stops with one clear reason instead of failing every case left. A case that stops before it is checked now counts as failed in the summary.
- The Docker image takes a `COMMIT` build argument, which `/api/v1/version` reports.
- The README's Quick start shows how to use Toskar from other devices on your network: turning on local network access and creating a key, restarting, finding the address, and the settings for a server without a browser.
- CI can run on self-hosted runners: the repository variables `CI_RUNS_ON` (pull request and push checks) and `TRUSTED_RUNS_ON` (releases, screenshots, and the small-model quality job) pick the runner, and GitHub's runners stay the default. The installer test always runs on a GitHub runner, since it installs system-wide.
- Settings keeps your own preferences, and the rest moved to where it's used (#203). **Find other computers** and the **External server** (advanced mode) are on the Computers page, under Network & access. **Connected services** (GitHub, Home Assistant, email, and calendar) are on the Tools page, next to tool sources. The API access summary card is gone from Settings; API Access already manages it. Settings' Network & access group is now Notifications.
- The sidebar is grouped by who uses each page (#203). **Use** has Chat, Automations, Knowledge, and Memory; **Customize** has Models, Tools, and Profiles & Orchestration; **Administer** has Train, Computers, API Access, Performance, and Diagnostics; Settings stays at the bottom. Tools and Profiles are always listed: advanced mode no longer hides whole pages, only expert controls inside them. Administer is closed until you open it, and stays as you left it; it opens by itself while one of its pages is showing. Page addresses are unchanged.
- The iPhone app's quality run now fails when its copies of core's
  text rules (what needs the web, small talk, search queries, memory
  commands) or its quality cases fall behind core's.
- The Tools page has three tabs: Your tools, Add more, and Images and video.
  Your tools lists every tool in a compact grid with search and filters;
  Add more has connected services and tool sources; Images and video sets
  up image and clip generation. How it works explains that tools are built
  in or added, and that each profile chooses which tools its chats may use,
  with a link to Profiles & Orchestration. A link to /tools?tab=add opens
  Add more.
- The Train page is titled Train, as in the sidebar, and its buttons match
  the rest of the app. Try an example and Build a new AI appear once: in the
  introduction when no AI is open, and at the top when one is.
- Files that belong to no chat are removed after a week, instead of
  piling up forever. That covers files an automation run, the API, or an
  MCP client made, and uploads never sent with a message. Files in a chat
  stay with the chat. The API shows when such a file goes as `expires_at`.
- `TOSKAR_WEB_FIXTURES`, for quality runs only, makes the daemon answer web search, places, and page reads from a file of fixed pages (`tests/quality/web.json`) instead of the internet, and log a warning that it does. The self-hosted real-model run uses it, so its answers are checked against the same pages as every other run.
- Chat looks things up on the web before answering when a message asks it to find something: a recommendation, where to buy, what something costs, reviews, a result or a release, or "check" and "find" for me. Before, these were answered from the model's memory, which for a small model often meant naming products that don't fit or telling you to search yourself.
- A follow-up such as "Can you provide a link to that bike?" is searched by what it refers to: the model writes the search from the conversation, so it names the bike.
- An answer that still tells you to search, check websites, or pretends to browse is looked up and written again from what the web says. Small talk such as "Hi! How are you?" is never looked up.

### Removed

- LLaVA 1.6 Mistral 7B left the catalog. It downloaded without its
  projector, so it couldn't see pictures in Toskar.

### Fixed

- The settings to paste into another AI app can be scrolled sideways from
  the keyboard.
- "Notify on change" notifies when something actually changed, not when the
  model words the same facts differently: it compares the values the
  automation tracks and the pages it read, and otherwise asks the model
  whether anything meaningful changed. The notification says what changed.
- "Notify when available" works in every language: the result now carries
  an availability flag instead of being read for English phrases.
- The Folder summary template now watches its folder, so its runs see the
  files; before, a run couldn't open files outside Toskar's workspace.
- An automation's history loads its newest 20 runs, with Show older runs for
  the rest, instead of every run it ever made.
- Preview answers in the automation's chosen language, and with its schedule's
  time zone, as the saved automation will.
- Automations run two at a time, so a slow one no longer holds up the
  others, and a run that takes more than 20 minutes is stopped instead of
  hanging.
- Run now starts the run and returns right away; closing the page no longer
  stops it, and its result appears when it finishes.
- An automation that fails three times in a row, for any reason, is paused
  with a notification saying why, in the App language. Retries go by the
  kind of error rather than its English wording.
- On Linux, the hardware check counts a memory limit set on Toskar's container or service (cgroup v2 `memory.max`, such as `docker run --memory` or systemd `MemoryMax=`), so model recommendations fit the memory Toskar may use instead of the whole machine's.
- An open chat has a page heading for screen readers.
- In the dark theme, dialogs and drawers dim the page behind them instead of
  covering it in a grey haze.
- Chats no longer pile up as active tasks. A chat's task was never moved past pending, so the Performance overview counted every chat ever sent as an active task and listed the oldest as Pending. A chat's task now ends completed, failed, or cancelled with the reply, and tasks left unfinished from before are settled when Toskar starts. Chats still raise no "task finished" notice.
- Remove from team asks first; it removed the computer at once.
- The Remove button for a model under Manage is readable in the dark theme.
- The copper Ratatoskr label in dark mode is a little lighter, so it meets
  the 4.5:1 contrast small text needs. The mascot's colors are unchanged.
- Profiles you made before image generation existed couldn't make
  pictures: a tool a profile doesn't list counts as off, and only the
  built-in profiles gained new tools. Your own profiles now gain new tools
  that stay on this computer and only read or make files in Toskar's
  store (pictures, clips, audio, files, drafting automations). Web,
  browser, terminal, file writes, and Git still wait for you to turn them
  on, and a tool you turned off stays off.
- Asking for a picture in a chat whose profile keeps image generation off
  says so, and where to turn it on, instead of listing websites. A request
  for a picture or clip no longer searches the web first.
- A chat's messages always load in the order they were sent. A reply could
  appear above the question it answered when both were saved within the same
  instant.
- On Windows, a graphics card's memory is read from the driver's registry entry, so cards with more than 4 GB are no longer reported as having 4 GB or less, and model recommendations use the real figure. Only graphics cards that are present are listed, so a card that was removed from the computer does not keep being reported.
- On Windows, a build from source now shows the web UI. The page used to stay
  blank because the scripts and styles under `/assets/` were answered with the
  start page instead of the files.
- On Windows, `make start` from PowerShell or cmd now builds `bin/toskar.exe`
  and runs it, and no longer prints "The system cannot find the path
  specified." when it looks up the git commit.
- Gemma models answer the question instead of the instructions. Gemma
  has no place for system instructions, so Toskar's (the date, the reply
  language, how to answer) reached it unmarked, as if the user had written
  them. Gemma 3 4B answered "Say hi in three words" with "You've provided
  the current date and time…". The instructions are now marked as coming
  from the app.
- A running model's speed is filled in from its latest replies, and its device is the one it runs on rather than the first graphics card found.
- Browse all models offers each repository's real model file instead of a
  guessed name. It skips speculative-decoding draft files, split parts,
  and extras in subfolders.
- Asking chat for a picture or a clip makes it. "Draw a dog", "please
  generate an image of a dog", or "are you able to make a picture of a dog
  for me?" used to get "I can't draw" or a list of stock-photo sites from
  a small model, though image generation was set up. Toskar now makes the
  picture itself, with the model only writing what to draw, and the same
  for changing an attached picture or making a short video.
- Pictures, clips, audio read aloud, files code saved, and page
  screenshots are attached to the answer that made them; only files
  created as documents were before.
- A request for a picture asked as a question, before image generation is
  set up, gets the setup offer with the request, so it's finished once
  the model is ready, instead of only "No, I can't generate images right
  now".
- On Linux, AMD and Intel graphics cards are found from the kernel's card list, without `lspci`, and an AMD card's memory is counted, so model recommendations use it. A card counts only when its render device is present, so a container without the card isn't told it has one.
- On Linux and Windows, llama.cpp now installs its Vulkan build when the
  computer has a GPU with a Vulkan driver, instead of always the CPU-only
  build. Chats that took minutes on the CPU now run on the graphics card.
  Computers without a usable GPU still get the CPU build.
- A llama.cpp install that has the CPU build is swapped for the GPU build
  the next time Toskar starts, or when you install llama.cpp again from the
  Runtimes page.
- llama.cpp reports a GPU backend only when the installed build has one, so
  Toskar no longer says it can use the GPU when it cannot.
- Installing llama.cpp again now replaces the whole install. Before, a
  reinstall could leave the old build in place.
- Running models say whether they're on the GPU again with current llama.cpp builds, which log the devices and offloaded layers only at trace verbosity and in a new format. Toskar now asks llama-server for that verbosity and reads the new format (#317).
- A model that can't call tools, such as Gemma 2, gets current answers again. Choosing one turned off every tool, including the web, places, and service look-ups Toskar runs itself before the model answers, so it answered from memory ("I don't have access to real-time information"). Those look-ups now run for every model; a model that can't call tools just isn't asked to.
- Read aloud in the Mac App Store app reads answers with your Mac's own
  voices, instead of trying to install speech and showing a sandbox error.
  The daemon now recognizes the App Sandbox from its folder too, so speech,
  text recognition, the code environment, image generation, training and the
  browser no longer try to download what the sandbox can't run.
- When speech can't be set up, Read aloud says so in plain words, without
  file paths.
- `go test ./internal/hardware/` passes on Windows: the Linux GPU tests,
  whose fake device paths Windows can't create, are skipped there (found
  by @black-operative).
- Links inside sentences show their text again: the Knowledge page link
  under a profile's knowledge, the install links for Node.js, uv, and Docker
  when adding a tool source, the Models link when a specialized AI's base
  model is not installed, and Open Chat after deploying one.
- "My daughter's birthday is May 3, remember that." is saved as a memory.
  Asking at the end of a message used to reach the model, which promised to
  remember without saving anything.
- Model names on the Installed and Running tabs are second-level headings,
  so screen readers no longer skip a level.
- A file listed under an answer's Sources, attached or made in the chat, can
  be downloaded from its chip, like the files under Files.
- "Make a PDF of that" and "can I have it as a Word document?" make the file,
  instead of telling you to paste the text into a word processor. If an
  answer still tells you to make a file yourself, Toskar asks the model once
  more to make it.
- Only the person who showed a device code sees whether a device connected
  with it, or cancels it.
- The live event stream no longer tells one person about another's chats,
  tool calls, memories, automations, or notifications: each person hears
  only their own, and everyone still hears about the computers and models.
- A long chat's summary is made as the person whose chat it is, so it
  works for people besides the Owner.
- "Forget that …" found nothing when the exact text didn't match, because
  its fallback search read the wrong columns.
- A request for a picture with the word misspelled, such as "make a
  picutre of a dog", makes the picture. When a request is worded in a way
  Toskar doesn't recognize and the model says it can't make pictures, or
  points to DALL-E, Midjourney, or ASCII art, Toskar makes the picture
  anyway.
- "Make" in a message no longer offers the terminal unless it means the
  build tool ("make test", a Makefile), so "make a picture of a dog" can't
  turn into a command that draws ASCII art.
- Saving a profile without changing its tools no longer turns tools on. The
  editor showed every tool a profile didn't list as Always allow, though
  the daemon treats them as off, so saving gave the profile every tool,
  including the terminal and git push. It also kept tools from connected
  services that the editor doesn't list, which saving used to drop.
- A question asking for a figure, such as "How much does it cost?", reads the best page it finds even at Fast effort, instead of answering from search snippets that rarely carry the price.
- With a tool that changes things available, the assistant calls it (it asks you first) instead of saying it can't and telling you to run a command such as `rm -rf` yourself.
- The Docker image includes the libraries the llama.cpp runtime needs (`libgomp1`, `libcurl4`), so a model the container installs can start.
- The source-type badge on the Knowledge page ("Linked", "Database") now meets WCAG AA contrast in the light theme; it measured 4.43:1.
- In the dark theme, red labels and buttons are easier to read: the red is a
  little brighter, so text such as a blocking problem with a training example
  meets the contrast standard on its tinted background.
- A training run's progress bar has a name for screen readers, and the
  example request on the Deploy step can be scrolled from the keyboard.
- A question about the weather in a month or season, such as "Juneau in
  June", is answered with typical figures from climate pages, not today's
  reading. If a word could be a month or part of a place name, the answer
  says how it read it.
- What the model writes before using a tool, such as "Let me find that
  information for you.", no longer appears in the answer.
- A weather answer links the wttr.in forecast page, such as wttr.in/juneau,
  instead of the percent-encoded query Toskar reads current conditions from.
  The Sources list does the same.

### Security

- A run started by what a trigger delivered (a webhook's body, a changed
  page, new feed posts, or changed files) can't use tools that change
  things outside Toskar, even ones approved for that automation: someone
  else wrote what it read. Those tools are skipped and reported, as
  unapproved tools are.
- Traffic between your computers is encrypted. Bifrost speaks TLS, and
  each computer is checked against the key it paired with, so another
  machine at its address is refused. Chats placed on another computer,
  their answers, tool jobs and their files, and training runs no longer
  cross the network readable. Once a computer has used encryption, it is
  never reached without it again.
- A paired computer still on an older Toskar keeps working over plain HTTP,
  and the Computers page marks it **Not encrypted** until it's updated.
- The API on port 7331 answers HTTPS. Phones, browsers, and apps on your
  network can use `https://` with the same port, so keys and chats no
  longer cross the network readable. Toskar makes and keeps its own
  certificate, and API Access shows the start of its fingerprint to compare
  with what a browser shows. You can use your own certificate with
  `api_tls_cert` and `api_tls_key` (or `TOSKAR_API_TLS_CERT` and
  `TOSKAR_API_TLS_KEY`). Plain HTTP still works from this computer and, for
  now, for phones on an older app.
- Websites open in a browser on this computer can no longer use or read the Toskar API on `127.0.0.1`. The daemon now answers browsers only from its own web UI, the desktop app, and loopback addresses on its own port, or when the request carries an API key, and no longer sends `Access-Control-Allow-Origin: *`. It also refuses addresses that are not this computer's over loopback, which blocks DNS rebinding. toskarctl, the iPhone app, and other devices with a key work as before.

## [1.6.1] - 2026-10-04

This release carries the iPhone app's text for its on-device chat, so the iPhone release built from it can show it. The engine, API, configuration, and data are unchanged from 1.6.0: the client contract stays 1.6, and pairing works with computers on 1.5.0 and 1.6.0. Binaries and the apt repository are not signed.

### Added

- The iPhone app's text for on-device chat: looking up current information on the web with its sources, Auto choosing a model on the phone and falling back to a smaller one, the context ring and its breakdown, memories kept on the phone ("Remember that …"), and their switches in Settings. It is in English; other languages show English until they are translated.

## [1.6.0] - 2026-10-03

Yggdrasil is now Toskar: the engine, programs, packages, repository, and Go module (`github.com/yeixio/toskar-core`) take the new name, and Yggdrasil stays in the story as the world tree. Chat reopens the conversation you left, with its context gauge, and the iPhone and desktop apps get the text for Auto, chat options, and a clearer message when another copy is running. Existing installs keep working without changes: their data folder and database stay where they are, and the old program names, `YGGDRASIL_*` variables, API keys, headers, MCP names, and Linux service name still work. The API, configuration, and data are compatible with 1.5: the client contract is 1.6, which adds an optional field, and pairing works with computers on 1.5.0. Go code that imports core must use the new module path. Binaries and the apt repository are not signed.

### Added

- The desktop app's catalog has the text it shows when another Toskar already runs on this computer at the address it needs, in all ten languages, so the app can say so instead of quietly showing the other one's models and chats.
- The iPhone app's chat text has Auto, the chat options (profile, run on, effort, and memory), and the model Auto used, in all ten languages, so the iPhone can offer the same chat choices as the web UI.

### Changed

- The client contract is 1.6: an answer's metadata can carry `context`, how full the model's window was for that answer, so clients can show the context gauge when a chat is opened again.
- Release archives are named `toskar-<version>-<os>-<arch>-headless.tar.gz`. The install scripts put Toskar in `~/.local/lib/toskar` or `/usr/local/lib/toskar` on macOS, run by the launchd service `ai.toskar.toskar`, and in `%LOCALAPPDATA%\Programs\Toskar` on Windows, started by the scheduled task Toskar. Installing over a setup from before the rename removes its old service or task, so only one copy runs, and leaves the old install folder as a link to the new one. The scripts can still install a release from before the rename.
- The programs are now `toskar` (the daemon) and `toskarctl` (the command line). `yggdrasil-daemon` and `yggctl` are installed as links to them, so launchd agents, systemd units, scripts, and MCP settings that run the old names keep working; on Windows, the install script adds them. Shell completion works for both names. The join command a computer prints still says `yggctl join`, so it works on a computer with an older version.
- The Linux desktop entry and icons are named `toskar` (`toskar.desktop`, `apps/toskar.png`). The logo files in `docs/brand` are named `toskar-*`; the mark, Yggdrasil the world tree, is unchanged.
- The web UI keeps its saved API key, language, effort, and settings in this browser under Toskar names, and moves them from their Yggdrasil names the first time it opens, so nobody is signed out or loses settings. Signing in to a tool source in a separate window works with computers on either side of the rename.
- The app says Toskar, in all ten languages: the window title, the sidebar, onboarding, settings, notifications, errors, and the iPhone app's text. Yggdrasil stays the world tree in the lore: the stories of the logo, Ratatoskr, the Norns, Mímir, and Odin. The iPhone app's About text says where the name comes from: Ratatoskr, who carries messages along the world tree.
- New installs keep their data in a Toskar folder (`~/Library/Application Support/Toskar`, `%LOCALAPPDATA%\Toskar`, or `~/.local/share/toskar`) and database (`toskar.db`). An existing install keeps using its Yggdrasil folder and `yggdrasil.db` where they are; nothing is moved.
- Environment variables have new names starting with `TOSKAR_`, such as `TOSKAR_API_PORT`, `TOSKAR_API_KEY`, and `TOSKAR_URL`. The `YGGDRASIL_` names keep working, so existing Docker, systemd, launchd, and MCP settings need no change. When both are set, the `TOSKAR_` name wins. The install scripts accept both names.
- Messages from the daemon, `toskarctl`, the install scripts, and notifications say Toskar: errors, the ntfy and desktop notification titles, email subjects, and the version notice (`Toskar Core`). The assistant answers questions about itself from Toskar's user guide, and still recognizes them when they say Yggdrasil. The `product` field in `/health`, `/about`, and `/version` stays `Yggdrasil` within contract 1.x.
- API responses carry a `Toskar-Contract` header beside `Yggdrasil-Contract`, and clients may send `Toskar-Client-Contract` or `Yggdrasil-Client-Contract`. Webhooks are signed in `Toskar-Signature` and `Yggdrasil-Signature`, and carry `Toskar-Notification-Id` and `Yggdrasil-Notification-Id`, with the same values, so existing receivers keep working.
- Over MCP, Toskar introduces itself as `toskar`, both as a server to other AI apps and as a client to tool sources, and signs in to services as Toskar. API Access → Use in other AI apps gives settings that name the server `toskar`; entries made earlier under `yggdrasil` keep working. A tool source can't be named `toskar` or `yggdrasil`.
- Community ratings fall back to the public summary at its new home, `yeixio/toskar-model-data`, when the ratings service can't be reached. Earlier versions keep working through GitHub's redirect from the old name.
- Requests to other services (ratings, place search, Hugging Face, web search, webhooks, ntfy, remote memory, and downloads) identify themselves as Toskar in their `User-Agent`.
- The repository is now [yeixio/toskar-core](https://github.com/yeixio/toskar-core), and the Go module path is `github.com/yeixio/toskar-core`. Old links, clones, release downloads, and the apt source keep working through GitHub's redirect. New Homebrew installs use `brew tap yeixio/toskar https://github.com/yeixio/toskar-core`; a tap added as `yeixio/yggdrasil` keeps working.
- The OpenAI-compatible API takes the assistant's controls in a `toskar` object. The `yggdrasil` object still works, and `toskar` wins when a request sends both. Responses and streamed chunks carry the answer's sources, steps, and progress under both names.
- The Linux packages are now `toskar` (deb and rpm), and the service is `toskar.service`, which also answers to `yggdrasil.service`. `apt-get upgrade` and `dnf upgrade` move an existing `yggdrasil` install over, keeping its data, its system user, and its service running; a small transitional `yggdrasil` deb makes that work with apt and can be removed afterwards. The service's log is in `journalctl -u toskar`.
- The Homebrew formula is now `toskar`, and `brew upgrade` moves an existing `yggdrasil` install to it.
- **Yggdrasil is now Toskar.** The engine is Toskar Core, and the apps are Toskar Desktop and Toskar Mobile; the site is [toskar.ai](https://toskar.ai). Yggdrasil stays in the story, as the world tree Ratatoskr runs along, and Toskar is named after him. An existing install keeps working without changes: its data folder and database, `YGGDRASIL_*` environment variables, `yggdrasil-daemon` and `yggctl`, `ygg_` API keys and `ygj_` join tokens, the `Yggdrasil-*` headers, the OpenAI `yggdrasil` object, MCP settings that name `yggdrasil`, the `yggdrasil.service` name, and pairing with computers on 1.5.0 all still work. The `product` field in `/health`, `/about`, and `/version` stays `Yggdrasil`, and `owned_by` in `/v1/models` stays `yggdrasil`, within contract 1.x.
- The docs, README, user guide, API description, and project policies say Toskar, and links point to toskar.ai.

### Fixed

- Going to another page and back to Chat reopens the chat you were in, instead of starting a new one, unless you were away for more than 30 minutes. The context gauge keeps its reading when you come back, and after a restart it shows the reading saved with the chat's latest answer.

## [1.5.0] - 2026-10-03

Yggdrasil in ten languages, a UI that meets WCAG 2.2 AA and works on a phone's browser, an assistant that answers from its own user guide and knows the date, a context gauge that shows memory, an optional OpenAI-compatible external server, and fixes for leaks found by new checks. Existing API routes, configuration, and data are compatible with 1.4: the changes add routes, optional fields, and database tables, and migrations run automatically. **Pairing is not compatible with earlier versions**: it is now signed end to end, so update Yggdrasil on every computer before pairing them or using them together. The removed Orchestrators page redirects to Profiles & Orchestration. Binaries and the apt repository are not signed.

### Added

- Answers with code are checked before they're shown: Go, JSON, Python, and shell code is parsed (never run), and code that doesn't parse goes back to the model with the errors named. At Thorough effort, long answers are also checked for statements that contradict each other or their sources, and rewritten once when they do. The answer's steps say what was checked, and its note says what's left.
- Describe an automation in the App language, not only in English: German, Spanish, French, Italian, Brazilian Portuguese, Japanese, Korean, and Simplified and Traditional Chinese. Schedules, weekdays, times of day, 24-hour times (18:30, 18 h, 18時, 오후 6시, 下午6点), intervals, one-time runs, and notify conditions all read, and the request box shows its example in the App language. English still works in every language.
- A price check keeps the currency you wrote, such as 500 €, R$ 2.500, 5万円, or 50만 원, asks for the price in that currency, and shows it in the name and notices. You can change the currency next to the amount. An amount without one is in the App language's usual currency.
- Run code in a sandbox. `code.execute` runs Python with numpy, pandas, and matplotlib for calculations, analysis, and charts, with no network, no access to your files beyond those from the chat it is given, and a 90-second limit; charts and files it writes are attached. It runs only in the operating system's sandbox (`sandbox-exec` on macOS, bubblewrap on Linux) and is unavailable elsewhere; there is no unsandboxed fallback. It asks first by default.
- Groundwork for Yggdrasil in other languages (#56). The UI's text now comes from a shared translation catalog in `i18n/`, used by the web UI, the desktop app, and the iPhone app. Settings has an App language: System default, or a language from the catalog; the daemon keeps it (`ui_locale`), so every app shows the same language. A system in a language without a catalog yet sees English text with its own date and number formats. The navigation is the first part translated. In advanced mode, the `en-XA` pseudo-locale shows every translated string accented and padded, so text that is not translated or does not fit stands out. Tests check the catalog in CI: valid JSON, no duplicate keys, the same keys, placeholders, and plural forms as English, and no key used in the code missing from English.
- The desktop app's menus, tray, and closing screen come from the shared catalog (`i18n/locales/<language>/desktop.json`) and follow the App language; the web UI tells the desktop shell when it changes. With an older desktop app the menus stay in English.
- The iPhone app's text is in the shared catalog (`i18n/locales/<language>/mobile.json`), so it can be translated with the rest of Yggdrasil. The app follows the connected computer's App language (`ui_locale`) unless one is chosen on the phone.
- Word documents and PDFs. `files.create` writes `.docx` and `.pdf` from Markdown, with headings, styled text, lists, tables, code, and quotes; `document.create` and `pdf.create` reach it in that format. PDFs use the standard fonts, so characters outside Western European text show as `?`.
- Spreadsheets with several sheets and formulas: in the CSV for an `.xlsx`, a line `## Sheet: Name` starts another sheet and a cell starting with `=` is a formula. `spreadsheet.create` reaches `files.create` in that format.
- `spreadsheet.analyze` summarizes a spreadsheet in the chat column by column (type, counts, minimum, maximum, average, total, or the most common values) with the first rows, so answers can use a whole file.
- Tool descriptors. Every tool has a version, an input schema, its outputs, a permission level from 1 (low risk, on this computer) to 4 (runs commands or code), its requirements, time limit, provider, and health. `GET /api/v1/tools/{id}` returns one; the Tools page shows them.
- Tool audit. Every tool call is recorded with what became of it, how it was allowed, how long it took, and what it was about. `GET /api/v1/tools/runs` lists them, and the Tools page shows each tool's recent calls. Records expire with run records.
- Profile strategies. A profile can work Auto (the default), as a Single model, as Planner + workers, or as a Team. Team now runs on the same pipeline as every other chat: a planner splits the request, workers write notes for each part, the answering model writes the answer with tools, memory, and knowledge, and a reviewer checks it. Quick questions are still answered directly. Programming uses the Team strategy.
- Workers on other computers. With the Team strategy or a worker model, each part of a plan has its own worker, which Norn can place on a paired computer, and parts on different computers are written at the same time. The chat timeline and run details show each worker's model and computer.
- Model roles. A profile can assign primary, fast, coding, planner, worker, and reviewer models, each with an optional computer. With the chat on Auto, a profile's coding model answers coding requests and its fast model answers quick questions. A fallback order lists the models to try when the answering model fails.
- Planning: Always asks the planner model to split a request that has no obvious parts.
- Placement rules per profile. Each paired computer can be Preferred or Never use, and Only this computer keeps every turn here.
- Profiles & Orchestration. The Profiles page is renamed, and its editor is grouped into Profile, Models, Tools, Memory, Orchestration, and Execution. Each built-in profile has Reset to defaults (`POST /api/v1/profiles/{id}/reset`), and Duplicate now copies a profile's orchestration and knowledge sources.
- Run details label each role (Planner, Worker 1, Answer, Reviewer), list them in that order, and count the model calls.
- Retry on another computer. When a model fails before showing anything, the turn runs again with the same model on another online computer that has it, before trying another model. Retries (1–3) sets how many times.
- The network advertisement (`_localai._tcp`) now says where the API is, as `api_port` in its TXT record. The service's own port is the computer-to-computer port, so an app that finds Yggdrasil on the network, such as the iPhone app, had to assume the default API port.
- Email and webhook notifications. Settings → Email, push, and webhooks sends notifications to your own SMTP server or to a webhook, with the categories and lowest severity each one receives. Webhooks are signed (HMAC-SHA256, a secret per destination, shown once) and must use HTTPS outside your network; SMTP passwords are kept in the secrets directory. A Test button sends one right away. Deliveries are recorded in What left this computer.
- Push notifications through ntfy, on ntfy.sh or your own server, for Android, iPhone, and browsers, with no Yeix-hosted service. Severity sets the priority, tapping opens Yggdrasil when its address is set, and on ntfy.sh only a generic notice is sent unless you choose full content.
- Delivery retries. A failed email, push, or webhook delivery is retried after 1, 5, and 30 minutes, without rerunning the task; a failure that retrying cannot fix stops at once and says what to change.
- Quiet hours. Desktop notices, email, push, and webhooks wait overnight and go out when quiet hours end; errors still go out unless you choose Hold everything.
- Health notifications on changes only: a paired computer going offline and coming back, and a model that crashes twice within 30 minutes (at most once an hour, with a hint when it is out of memory). They go to the bell and to destinations that take the Health category.
- The notification center filters by category, counts repeats ("3 times"), and says when a notification is held or was not delivered everywhere. `GET /api/v1/notifications/{id}` shows each channel's delivery. The client contract is now 1.1.
- Browser use: `browser.open`, `browser.extract`, and `browser.screenshot`, and, asking first, `browser.click`, `browser.type`, and `browser.download`, in an isolated headless browser for each chat (the Chrome, Edge, Chromium, or Brave already installed, with a fresh, temporary profile, never your own). Every request is checked so pages cannot reach this computer or the local network, password, payment, and one-time-code fields are never typed into, and each page opened is recorded in What left this computer. A Browser capability turns it on or off in a profile.
- A shared model rating can include how the model runs on your computer: its typical speed and time to first token, how many of its starts worked, and whether it crashed or ran out of memory. This is a second choice in the rating dialog, off by default, and the dialog shows the numbers before you share. Community ratings show the typical speed and crash rate others reported.
- With community ratings on, recommendations weigh how people with computers like yours rate each model: a clearly better rated model can be suggested ahead of the usual first choice, and a poorly rated one gives way. Ratings from only a few people count less, models that can't run on your computer are never suggested because of ratings, and a model chosen this way says so.
- Community model ratings: rate a model with 1 to 5 stars and optional reasons from its card, the Installed tab, or a prompt in chat after you've used it a while. Ratings stay on this computer unless you choose to share one, and the rating dialog shows exactly what sharing sends: the model, your computer's class, and a random rating ID. With **Show community ratings** on in Settings (off by default), models show how people with similar computers rate them, and how everyone does, with how many ratings and a label when there are few.
- Help translating Yggdrasil: CONTRIBUTING.md explains how to fix or review a language by pull request, and a Translation issue form takes reports of wrong text and requests for new languages. `scripts/i18n.py status` shows what a language still lacks, and `scripts/i18n.py glossary <language>` prints the words a language uses for Yggdrasil's main terms, read from its catalog (`i18n/glossary.json`).
- The context gauge says how much memory the conversation window reserves on this computer, such as "about 1 GB", and that a longer window remembers more and uses more memory. Near the limit it says the oldest messages will be sent as a summary and suggests a new chat.
- docs/design.md describes how the UI is built: color tokens with their contrast in both themes, type, spacing, the components and React helpers to reuse, the order of loading, failed, empty, and loaded states, keyboard patterns for dialogs, menus, and tabs, phone-width layout, accessibility rules and the CI check, writing and brand rules, and a checklist for new UI. The pull request template links it.
- Email and calendar with your own accounts and an app password (no Google or Microsoft sign-in app). Email (IMAP and SMTP: Fastmail, iCloud, Proton Mail Bridge, Nextcloud, your own server, and Gmail or Outlook where app passwords are allowed) adds `email.search`, `email.read` (without marking messages read), and, asking first, `email.draft`, `email.send` (threaded replies, a copy in Sent), and `email.archive`. Nothing is deleted. Calendar (CalDAV) adds `calendar.search` and `calendar.availability`, and, asking first, `calendar.create`, `calendar.update`, and `calendar.cancel`, which keep attendees and alarms and never overwrite an event changed elsewhere. Connect them in Settings → Connected services.
- Connect an OpenAI-compatible server, such as OpenAI, or vLLM or Ollama on another machine, in Settings → External server (advanced mode). Its models appear for a chat to choose, marked external, and chats with them are recorded in What left this computer. Auto never picks them, and offline profiles and chats using memories or knowledge marked This computer only refuse them.
- Guided installation: asking for something Yggdrasil can't do yet but can install, such as "Make me an image of a Viking tree" before image generation is set up, is answered with what would be installed, its download size, and the computer it would run on. A card in the chat sets it up, shows the download, and then finishes the request. "Can you generate images?" offers the same card. The capability inventory lists these setups (`setups`).
- Assistant language in Settings → Language: answers come in the language you write in (the default), in the App language, or in a language you choose, apart from the App language. Asking in a message, such as "answer in English" or "antworte auf Deutsch", always wins. The language is told apart on this computer; nothing is sent anywhere to find it out.
- Automations choose the language their results are written in: the assistant language (the default), the App language, the language of the request, or a language you pick, under Advanced in the automation form. A language the request asks for, such as "answer in English", always wins.
- `pnpm lint` in `web/` fails on hard-coded text in the UI, so new text goes through the translation catalog.
- BGE-M3 in the model catalog: a multilingual embedding model that lets knowledge and memory search find passages by meaning in more than 100 languages, such as a Spanish question finding an English document. When it is installed next to an English embedding model, it is the one used.
- Knowledge sources record the languages they are written in, and the Knowledge page shows them. A source in another language than the App language says when a multilingual embedding model would help questions find it.
- Memories are found across languages: with an embedding model installed, a memory saved in Spanish ("Mi proyecto usa Go") comes with a question in English ("What language does my project use?"), and the other way round. Each memory records the language it is written in, and the Memory page names it when it differs from the App language.
- Models say how well they write each language: on the Models page, a card shows its level in your App language (Limited, Fair, Good, or Excellent) and how many other languages it has one for, and its details list every language with the confidence and where the level comes from. The API's catalog models carry `languages`.
- Notifications in the API carry `message`, their title and body as translation keys with the values they need, and webhooks also get `language`. The client contract is 1.4.
- Ratings by language: rating a model asks how well it worked for you in a language, your App language unless you choose another or not to say. A model card shows its community ratings by language, your App language first, and with community ratings on, Auto counts them when choosing a model for an answer in that language. The API's `PUT /models/{id}/rating` takes `language`, and `/ratings/community` returns each model's `languages`.
- The web UI lays out right to left for right-to-left languages such as Arabic and Hebrew: the sidebar, drawers, menus, and arrows mirror, while code, commands, and logs stay left to right. A right-to-left pseudo-locale (`ar-XB`), chosen in Settings in advanced mode, shows layout that does not mirror.
- `scripts/i18n.py status` counts only text that is probably untranslated: text meant to read the same as English, such as `PDF` or German `Name`, is listed in `i18n/same-as-english.json`, and CI says when a listed key is translated or gone.
- Yggdrasil in German, Spanish, French, Italian, Brazilian Portuguese, Japanese, Korean, and Simplified and Traditional Chinese: the web UI, the desktop app's menus, and the iPhone app. The translations are machine made, and Settings says so under the language picker until each is reviewed. A system set to Chinese (Taiwan or Hong Kong) or Portuguese (Portugal) gets the closest one.
- Pasted training examples can use the question and answer labels of these languages, such as `P:`/`R:` in Spanish or `问：`/`答：` in Chinese.
- Speech tools say which languages they work in (`languages`, and `auto_detect` for Whisper) in `GET /tools/providers` and in Diagnostics, and a speech call goes to a paired computer whose provider works in its language.
- Images on this computer: `image.generate` makes an image from a description, and `image.edit` changes an image in the chat from an instruction, with stable-diffusion.cpp and FLUX.2 [klein] 4B. Image generation is set up once from the Tools page, which recommends a model for the computer's memory (5.2 GB, or 8.8 GB for more detail), downloads it with progress, and can stop, resume, and remove it (`/api/v1/images/setup`). Downloads are pinned and checked; prompts and images are not sent anywhere. It runs on macOS with Apple silicon, and on Linux and Windows on the CPU.
- Chats accept PNG and JPEG images, which are shown in the chat and can be edited. Images the assistant makes are shown too.
- An Images capability in profiles turns image generation on or off. Offline profiles keep it.
- One command installs Yggdrasil and joins a computer to your network. `yggctl join-token create` now also prints an install-and-join command: `install.sh` on Linux and macOS, and `install.ps1` on Windows, both attached to each release. They install the release for that computer, check it against the release's checksums, start Yggdrasil as a service (systemd, launchd, or a scheduled task), and join. A Yggdrasil that is already installed and running is left as it is.
- `yggctl join --name` renames the computer as it joins, and `--wait 60s` waits for Yggdrasil to start first, for provisioning scripts. The Clustering guide has cloud-init and Ansible examples.
- The Computers page can add a computer by command: **Add by command** makes a one-time join command to run on the new computer, for one with Yggdrasil, one to install it on, or Windows, with a copy button, a countdown, revoke, and the recent commands. It says when the computer has joined.
- Diagnostics shows how much memory Yggdrasil itself is using and its background tasks, with the last day as a small chart. If memory grows steadily for hours, it says so and suggests exporting diagnostics to report it.
- CI checks the web app for memory leaks by moving between every page many times, and a soak test (`YGGDRASIL_SOAK=1 go test ./tests/soak`) runs a throwaway daemon through hundreds of chats, cancellations, and event connections and fails if memory, goroutines, or open files keep growing.
- Email, push, and webhook destinations can send a daily digest instead of each notification: choose **Daily digest** and a time. Errors still go out right away, and a digest due during quiet hours waits for them to end.
- Join a computer to your Yggdrasil network with one command. On a computer in the network, `yggctl join-token create` prints a command; run it on the new computer, for example over SSH, and the two trust each other with no pairing prompt, mDNS, or configuration. The token lasts 15 minutes, works once, can be revoked, and never crosses the network: the command carries the first computer's fingerprint, so a different machine at that address is refused. `yggctl network` shows the computers this one trusts, and `yggctl leave` leaves the network.
- Places and directions with OpenStreetMap: `places.search` finds a kind of place near somewhere (cafes, pharmacies, fuel, parks, and more) or a place by name, with addresses, opening hours, and distances in km and miles; `places.details` looks one up; `maps.route` gives driving, walking, or cycling directions; and `maps.distance` says how far and how long. They are part of the Internet capability. Each request is recorded in What left this computer as Maps and places, a repeat within the hour is answered from memory, and the services can be your own (`places_geocoder_url`, `places_overpass_url`, `places_router_url`).
- Diagnostic bundles include a runtime summary and goroutine and heap profiles, so a report of growing memory can be diagnosed. They hold function names, counts, and sizes, no prompts or files.
- `YGGDRASIL_PPROF=127.0.0.1:6060` serves Go's live profiles on this computer for developers; only loopback addresses are accepted. See Troubleshooting: "Yggdrasil uses more and more memory".
- Tools on other computers: image generation and speech run on whichever paired computer can run them, preferring one whose GPU does the work, so a laptop without image generation can ask for an image and a paired workstation makes it. The asking computer keeps the approval, the audit, and the files; the other computer runs the work and keeps nothing. The chat profile's computer policy applies, an unreachable computer is skipped for the next, and Stop stops the work there. Each job sent is recorded in What left this computer.
- Diagnostics → Tools on each computer shows each computer's image and speech providers: ready, installing, failed, or not set up, and whether a GPU does the work (`GET /api/v1/tools/providers`).
- Speech on this computer: `speech.transcribe` writes down what an attached audio file says (Whisper, fast or accurate), and `speech.synthesize` reads text aloud as an audio file (Piper). Answers have a Read aloud button (`POST /api/v1/speech`). Chats accept audio files (`.wav`, `.mp3`, `.m4a`, `.aac`, `.ogg`, `.flac`, `.webm`), which play in the chat. The first use installs speech from PyPI and downloads the model or voice from Hugging Face; audio and text are not sent anywhere.
- A Speech capability in profiles turns transcription and reading aloud on or off. Offline profiles keep it.
- Video on this computer: `video.generate` makes a clip of a few seconds from a description, or brings an image in the chat to life, with stable-diffusion.cpp and Wan 2.2 TI2V 5B. Video generation has its own setup on the Tools page and offers in chat (an 8.5 GB download; `/api/v1/video/setup`), runs on a paired computer that has it, and clips play in the chat. A Video capability turns it on or off in a profile.

### Changed

- CI checks every page for accessibility problems. It runs axe-core in a real browser over each page with demo data, in light and dark themes at desktop and phone widths, and fails on contrast, labeling, or structure problems (WCAG 2.2 AA). Run it locally with `node scripts/screenshots/a11y.mjs`; see docs/development.md.
- Auto weighs community ratings and load. With community ratings on, a model people with computers like yours rate clearly better is chosen over a larger one. A model busy answering something else gives way to a similar idle one that's already loaded, and work goes to an idle computer before a busy one. The answer's steps say when ratings or load decided.
- The separate Team orchestrator is gone. Profiles saved with it, and API requests that name it, move to the Team strategy with their roles and models; `coordinator` becomes `planner`. A profile's role models are now used in chat: before, the model chosen in the chat replaced them all, except in Team profiles.
- Pull requests describe their changes in `changes/unreleased/` instead of editing `CHANGELOG.md`, so they no longer conflict over the changelog. `scripts/changelog.py` checks the fragments in CI, previews the next release, and writes them into `CHANGELOG.md` when a release is cut.
- Every Go package's tests fail if they leave goroutines running (`internal/leakcheck`), so leaks like these are caught before they ship.
- The client contract is 1.2: answers can carry `setup`, an offer to install what the request needed.
- The notes shown with an answer are in the App language: figures Yggdrasil could not confirm, an answer that claims a change no tool made, a stopped answer, a smaller model answering after another failed, and a small model answering from your files or knowledge. They were in English in every language.
- The labels on an answer's sources ("Attached file", "Made in this chat", "Memory") are in the App language, including for apps that use the OpenAI-compatible API.
- The steps listed with an answer are in the App language: what Yggdrasil looked up, read, ran, and checked, why Auto chose the model, and which model or computer answered after another failed. They were in English in every language.
- Automations, notifications, Tools, connected services, and API Access are in the shared translation catalog (`automations.json`, `notifications.json`, `tools.json`, `services.json`, and `apiAccess.json` under `i18n/locales/<language>/`), so they follow the App language: schedules, notices, the notification bell and desktop notices, email/push/webhook delivery, tool sources, image generation setup, and API keys. Automations still read requests written in English, such as "every morning at 8:00 AM".
- Chat's text is in the shared translation catalog (`i18n/locales/<language>/chat.json`), so it follows the App language: the composer, history, progress messages, tool permission prompts, errors, answer details, run details, and file chips. Plural forms, such as "Used 2 tools", come from each language's plural rules.
- Errors from Yggdrasil show in the App language. The service sends each error with a stable code, such as `MODEL_NOT_INSTALLED` or `MEMORY_LOOKS_SECRET`, and the values its message needs, and the web UI shows the text for the code, in all ten languages; the English text stays under Details for Diagnostics. Chat recognizes errors such as a model running out of memory or a computer going offline by their code instead of by their English wording. Setting an invalid value in Settings now says which setting and value, instead of "Could not update settings."
- The client contract is 1.3: a chat that fails while streaming sends `event: error_code` with the code, before `event: error` with the text, which older apps still read.
- Numbers, dates, times, sizes, speeds, prices, and percentages follow the App language's region everywhere in the web UI: German shows 1.234 passages, 1,5 GB, 30.09.2026, and 45 %. "5 minutes ago" comes from the browser's own relative-time rules, so it reads naturally in every language.
- The startup screen, the API key prompt, and the stories behind the Norse names, the mascot, and the logo show their text in the App language.
- Knowledge, Train, Profiles, Orchestrators, and Diagnostics show their text in the App language. Counts on these pages use the language's plural forms, so the health page says "1 model installed" rather than "1 models installed".
- Auto picks a model that writes the answer's language well: asked in Spanish, a model rated good in Spanish comes before a larger one rated fair. A model without language ratings is still used. When nothing installed writes the language well, the answer is still in it, with a note saying it may read less well and that better-rated models are on the Models page.
- When a model stops responding or runs out of memory, chat says so in the App language.
- Models, Computers, and Performance are in the shared translation catalog (`models.json`, `computers.json`, and `performance.json` under `i18n/locales/<language>/`), so they follow the App language, including the model notes Chat shows when a request needs a model that can use tools or see images. Counts such as "2 models running" and "3 computers" use each language's plural rules.
- Notifications show in the App language: the bell writes each notice in the language you chose, and desktop notices, email, push, and webhooks are written in the App language when they are sent. Text a model or an automation wrote stays as it was written.
- Chat messages, answers, and memories take the direction of their own text, so Arabic reads right to left in an English UI and English left to right in an Arabic one.
- Run details (in advanced mode) show a failed run's error and the run's status in the App language. The English error text stays available on hover.
- The client contract is 1.5: a run trace has `error_code` and `error_details` beside `error`, the same stable codes as other errors, so clients can show the error in the App language.
- Run details (in advanced mode) show the strategy and effort in the App language: how the request was worked through, a specialized AI or automation that answered, and which model or computer answered after another failed.
- Settings, onboarding, and Memory are in the shared translation catalog (`settings.json`, `onboarding.json`, and `memory.json` under `i18n/locales/<language>/`), so they follow the App language. Counts such as "18 cores" and "7 days" use each language's plural rules.
- Automation schedules show times in the App language's clock, such as 18:30 in German.
- Read aloud speaks the text's language: a German answer is read by a German voice, not the English one. There are voices for 25 languages; text in a language without one, such as Japanese, says so instead of being read in the wrong voice.
- When a model runs out of memory because training is using this computer, chat says so in the App language, with about how long training has left and what to do. The explanation now shows even when the model's error was plain text, instead of only under Details.
- "Can you …?" answers no longer count a tool that is on but cannot run on this computer, and say what would make it work, such as setting up image generation.
- While a computer trains an AI, chats and other work go to a paired computer that has the model, and the answer's steps say which one answered. The Computers page marks a computer that is training. With no other computer that has the model, the chat is still answered here, as before.
- Settings says plainly that your data stays private, and what is encrypted. A new **Your data stays private** card at the top of Privacy explains that chats, memories, knowledge, and files stay on this computer with no account, cloud, or telemetry. It also says Yggdrasil doesn't encrypt its own files, so turn on FileVault, BitLocker, or LUKS to keep them encrypted. Online services use HTTPS, but traffic between your own computers isn't encrypted yet. The user guide and docs/privacy.md answer "Is my data encrypted?" and "Are my chats private?", and so does the assistant when you ask it.
- When a model can't answer, Yggdrasil tries another installed quantization of the same model before switching to a different model: a smaller one when it ran out of memory. The answer's steps say which version answered.
- Shared community ratings go to `https://ratings.toskar.ai` by default, and the daily ratings summary comes from there. The `ratings_url` setting still overrides it.
- The chat box is simpler. Model stays next to the message, and Profile, Run on, Effort, and Memory move into one Options button that shows what differs from the defaults, such as "General · Thorough · Memory off". On a phone the box is two rows instead of three rows of unlabeled menus. When Settings asks where chats run, Run on stays in view until you choose.
- The sidebar's status footer uses plain words (Computers, Model placement, Knowledge) and is headed Status, so it no longer shares the System heading with the menu above it. The realm behind each line is in its tooltip.
- Plainer wording where everyday users read it. Onboarding describes your hardware as "what this computer has for running AI models", not "hardware detected for local inference"; the Run on tooltip and the Knowledge page say what Yggdrasil does rather than naming Norn or Mimir in the sentence (the realm names still head each page); the sign-in screen and Diagnostics say "the Yggdrasil service" instead of "the daemon"; and a model's quantization is labeled "Compression (quantization)". In all 10 languages.
- While a page loads, it shows placeholders in the shape of what's coming (model cards, list rows, chat bubbles) instead of a "Loading…" line, so the page doesn't jump when the content arrives. Searches and other work in progress keep their spinner. Models says "Checking hardware…" while it reads this computer's hardware, instead of "Hardware details unavailable".
- `.webm` files are videos: they play as video in chat, and their sound can still be transcribed.

### Removed

- The Orchestrators placeholder page, which only said "coming soon" and wasn't in the sidebar. A link or bookmark to it opens Profiles & Orchestration, where each profile's way of working (its orchestration strategy) is chosen.

### Fixed

- Links in answers open. A model writing a page's address from memory often gets it wrong, so answers now link only to pages their sources gave: a guessed link goes back to the model to be replaced, and any left are named in a note under the answer, since they may not work. A site's home page is still fine. This applies wherever you chat, in the desktop app, the browser, and on a phone.
- The assistant knows today's date and the time. Questions like "what's on this Friday?" or "how long until 5 pm?" used to depend on the model's guess; now every answer gets the date and time in your time zone, which the app takes from your browser or phone. Automations use the time zone their schedule runs in. Nothing extra leaves your computer.
- The assistant knows Yggdrasil's own features and documentation. Ask how to do something in Yggdrasil, such as scheduling an automation, connecting a computer, or adding knowledge, and it answers from the user guide that ships with the app, naming the screens and buttons to use. Before, a model guessed. Questions about anything else are unchanged.
- The web UI in Brazilian Portuguese and Simplified and Traditional Chinese no longer falls back to English.
- An automation's amount accepts a decimal comma, such as 19,99.
- Yggdrasil Desktop: answer sources and other links that leave the app now open in the default browser. Signing in to an MCP tool source now works too: it opens in the browser and returns to the daemon's address, where before it was sent back to the app's own `wails://` address, which no browser can reach. Saving a chat file, an exported GGUF model, or a training sample now asks where to save and writes the file; a large model streams straight to disk. The desktop app's web view can't open windows or download files, so these go through the desktop shell (Yggdrasil Desktop 1.4 or later). In a browser, nothing changes.
- Yggdrasil Desktop on Windows: live progress, tool approval prompts, notifications, and chat replies as they are written now appear. Wails hands the app's web view a proxied response only once it ends, and the daemon's event stream never ends, so none of it arrived; replies showed all at once. The desktop shell now reads the event stream and relays each event to the page, and replies take their text from that relay (Yggdrasil Desktop with the event relay). The desktop app uses the relay on every platform; in a browser, nothing changes.
- The context gauge counts everything in the system prompt, including your personalization, memories, and guide excerpts, which it left out when estimating.
- Chats with an external server show the real prompt size: Yggdrasil asks the server for its token counts at the end of each reply.
- When no tokenizer is available, the estimate is closer for Chinese, Japanese, and Korean (about a token per character, not a quarter) and for code and JSON.
- The chat's context gauge uses the window the model is actually running with, read from llama-server, instead of a guess from the catalog that could differ from it. Switching back to a chat shows its last reading instead of an empty gauge.
- Connected services scrub only secret values, such as a token, from results. Every stored value of six or more characters was scrubbed, so a result mentioning the service's own address, such as a Home Assistant URL, read "[redacted]".
- The `external-openai` runtime could not be configured, so it always reported "not configured" though the documentation described it as working.
- Looking for nearby computers no longer risks returning a half-written list. The search now waits until every answer it heard has been read before handing back the results, instead of guessing with a fixed pause.
- The daemon starts again on builds where two database migrations shared number 031: knowledge source languages are now migration 032.
- Yggdrasil starts again on a build with both knowledge languages and shared model runtime observations. The two shipped database migrations with the same number, so the database refused to open; knowledge languages now has its own number.
- Cancelling training on a paired computer just as it starts now stops it there. Before, a cancel that arrived while the examples were still being sent could leave the paired computer training until its daemon restarted, holding its training slot. A cancel now waits for the paired computer to answer (up to 2 minutes), then stops the run there and removes its files.
- The sidebar header no longer runs the status chip ("No model", "Ready") into the notification bell. The status sits under the Yggdrasil title, and a label too long for the space is shortened, with the full hint on hover, in every language and in right-to-left layouts.
- Web page reads (`internet.open`) no longer open addresses on your own computer or local network, such as Yggdrasil's own API, a router's admin page, or a cloud metadata service. A web page could otherwise lead the assistant there and read the result. Every address a site's name resolves to is checked, again on each connection, so a redirect or a changing name cannot get around it.
- Checking email no longer leaves a background task behind for each IMAP connection. A daemon that checked mail on a schedule grew without limit.
- The browser tool's idle-session cleanup stops as soon as the last browser closes, including when Yggdrasil quits, instead of up to a minute later.
- A few words left in English are translated: the API key placeholder in Japanese, and the LoRA rank and epoch fields, the loopback address note, and a suggested prompt in Traditional Chinese.
- The .deb and .rpm packages of a pre-release, such as 1.4.0-beta.1, have a valid version again; it had the build machine's home folder in it, so apt and dnf refused them.
- Renaming a computer in Settings shows the new name to other computers right away, instead of after a restart.
- A computer that joins a network under a name already taken there takes
  the name the network gave it, such as "worker-01-2", so both computers
  show the same name.
- Renaming a computer while discovery settings change no longer leaves an
  old network announcement running.
- Asking the assistant how to install or add MCP (or "MPC") now gets the actual steps: Tools → Add tools and its four ways in, or, for using Yggdrasil from Claude Desktop, Cursor, or VS Code, where to find what to paste. Before, a model guessed, often telling people to edit another app's config file.
- Paired computers' fingerprints are stored in the same form join commands
  show; existing ones are updated when Yggdrasil starts.
- Stopping an MCP tool source ends the helpers it started, such as the `node` process `npx` runs, even when the server quits on its own. They used to keep running each time a source stopped.
- After a model ran out of memory, the fallback could pick a larger version of the same model; it no longer does.
- Text is easier to read. Faint labels, status chips, and accent colors now meet the WCAG AA contrast standard (4.5:1) on every surface, in both light and dark themes. Light-theme chips such as Ready and Offline were the hardest to read before.
- A page that runs into a problem now says so and offers Try again, and the rest of the app keeps working. Before, one page's error blanked the whole window.
- Each page names itself in the window title, such as "Models · Yggdrasil", so browser tabs, history, and screen readers say where you are.
- Screen readers get a clearer page: the sidebar is a header, the Main navigation, and a footer; empty states and computer cards use the right heading level; and the Default profile menu in Settings is labeled.
- Yggdrasil works fully from the keyboard. Dialogs (delete a chat, approve a tool, rate a model, a tight fit, local network access) and the chat history and phone menu drawers keep focus inside while open, close with Escape, and return focus to what opened them. Dialogs start on the safe choice, so Enter alone never deletes a chat or approves a tool, and Escape on a tool request denies it.
- Tabs (Models, Performance, Profiles, adding a tool source, sharing with apps, join commands), star ratings, and the menus on chats, models, and profiles follow the standard arrow-key patterns: one Tab stop per group, arrows to move, Home and End, and Escape to close a menu.
- The chat box and the chat-list search show when they have focus. The setup choices in onboarding say which one is selected.
- A page that can't load its data says so, with Try again and a link to Diagnostics, instead of looking empty. Before, a failed request showed "No computers", "No model installed", or "No tool sources yet", as if they were gone, or kept saying Loading. This covers Chat, Automations, Models, Train, Knowledge, Memory, Computers, Performance, Profiles, and Tools.
- The sidebar's status no longer says "No model" or "No computers" before it knows; while those are loading or can't be read, it doesn't claim either. A failed request is retried once instead of three times, so its error shows in about a second.
- Yggdrasil works in a phone's browser and in a narrow window. Below tablet width the sidebar used to stay full size and leave pages about 140 pixels, one word per line. Now it is a menu that slides in from a button at the top, and pages use the full width with a narrower margin.
- On a phone, the chat box no longer covers the conversation. In a chat it starts at one line and grows as you type, up to 30% of the screen; the empty start screen keeps its large box. The History and New chat buttons stay on one line instead of wrapping.
- The App Store and Google Play phone screenshots show the app as it is on a phone, with the top bar and menu, instead of a layout made only for the screenshots.

### Security

- Pairing two computers is signed end to end. Each computer proves it holds
  its key, and only the computer that was asked can finish a pairing, with
  the code that was shown. Update Yggdrasil on both computers before pairing
  them or using them together.
- Pairing codes are limited: repeated wrong codes or failed answers end the
  pairing, and you start again with a new code.
- Pending pairing requests are no longer listed to other computers on the
  network.
- Requests between paired computers name the computer they are for and are
  good only once.
- Pairing never replaces the key of a computer that is already paired. To
  pair a computer again with a new key, such as after reinstalling it,
  remove it on the Computers page first.

## [1.4.0] - 2026-10-02

Stable release of 1.4.0. It contains everything in [1.4.0-beta.1](#140-beta1---2026-10-01), the AI experience platform and the remaining Train Your Own AI items, plus the changes below. Existing API routes, configuration, and data are compatible with 1.3: the changes add routes, optional fields, and database tables, and migrations run automatically. NVIDIA (CUDA) training, PostgreSQL and MySQL knowledge sources, and the Mac App Store sandbox have not been tested on that hardware or in that build. Binaries and the apt repository are not signed.

### Added

- Documentation for 1.4.0. New [Configuration](docs/configuration.md) (data directory layout, every `config.json` key, environment variable, and setting) and [Command line](docs/cli.md) (`yggdrasil-daemon` flags, the Linux systemd service, and every `yggctl` command) pages. The API reference now lists every route, grouped by area, and every event type. The user guide adds sections on memory, knowledge, tools, notifications, profiles, privacy, and diagnostics.

### Changed

- `api/openapi.yaml` now describes every route: the control plane, the OpenAI-compatible API, and the MCP server, with request bodies, responses, the error shape, and bearer authentication. It previously covered 51 of them. A test fails when the daemon serves a route the spec does not describe, or the spec describes one the daemon does not serve.
- Rewrote the architecture, privacy, and troubleshooting pages for 1.4.0, and updated tools, capabilities, compatibility, and the README. Removed statements that no longer matched the code, such as where retrieved knowledge goes in a prompt and which `/chat` fields exist.
- Each release carries its own screenshots. After a release is published, the Screenshots workflow captures the README stills and the walkthrough from that release's interface and attaches them as `screenshot-<name>` files, which yggdrasil.yeix.io shows. Started by hand with a tag, it attaches them to an existing release.

### Fixed

- Chat answers keep their indentation. Nested lists stay nested and code blocks keep their indentation; only extra spaces in the middle of a line are collapsed.
- The Screenshots workflow passes again. Screenshot validation accepts the lowercase opaque value that ImageMagick 6 on Ubuntu prints, a screen that does not become ready is loaded once more before the run fails, and a failure now prints the page's errors and keeps what the page showed.

## [1.4.0-beta.1] - 2026-10-01

Beta pre-release of 1.4.0. It adds the AI experience platform and the remaining Train Your Own AI items. Existing API routes, configuration, and data are compatible: the changes add routes, optional fields, and database tables, and migrations run automatically. NVIDIA (CUDA) training, PostgreSQL and MySQL knowledge sources, and the Mac App Store sandbox have not been tested on that hardware or in that build. Binaries and the apt repository are not signed.

### Added

- Sandboxed Mac App Store builds can train on a paired computer. A sandboxed copy of Yggdrasil no longer tries to download Python; it says training can't run on this computer and chooses a paired computer running Yggdrasil Core. A store build can also ship the training and text-recognition environments beside the daemon, and `yggdrasil-daemon -python-envs` lists what to bundle.
- Versioned client contract. Events, run traces, and answer metadata (citations, steps, files) carry a contract version (`1.0`), every API response has a `Yggdrasil-Contract` header, and `/api/v1/version` describes the contract. Fields are only ever added within a major version; a test fails if one is removed or renamed. An app built for another major version gets a clear 426 that says which side to update.
- Caching with declared policies. Every cache says what it keeps, how long, what clears it, where it applies, and how private it is; credentials are never cached. Repeat web searches and page reads within minutes are answered from memory, so nothing leaves the computer again. The capability inventory is cached and refreshed when models, computers, or tools change, and Hugging Face searches use the same cache. Diagnostics lists the caches in advanced mode, run details show cache hits, and deleting run records clears personal caches. Routes are under `/api/v1/caches`.
- Train on NVIDIA GPUs. A computer with an NVIDIA GPU can now train specialized AIs with PyTorch, using LoRA, or QLoRA when the GPU's memory is tight. Training fit uses the GPU's own memory, and a Mac can send training to a paired NVIDIA PC. The first run installs PyTorch with the CUDA libraries for the computer's driver (about 4 GB).
- Knowledge from databases and web APIs. Connect a SELECT query on a SQLite file, PostgreSQL, or MySQL, or a URL that returns JSON, CSV, or text, and each row or item becomes a passage. Yggdrasil only reads: queries run read-only. Data older than the chosen interval (5 minutes to a day) is fetched again when a question uses it, and if a fetch fails the last data keeps answering. Passwords and tokens are stored apart from the database and never shown again.
- Export a specialized AI as one GGUF file. The Deploy step merges the trained revision into its base model, so LM Studio, Ollama, llama.cpp, and other GGUF tools can run it without Yggdrasil. The file is a little larger than the base model, and the AI's instructions are shown to copy as the system prompt. A notification says when a large export is ready.
- Quality test set. Ten representative requests, each with the behavior it must have, run against the stub model on every change and against real models with `make quality-real` or a weekly self-hosted workflow. The behavior checked includes a simple question staying direct, a price question using knowledge, a risky command asking first, a long conversation remembering an early fact, a current question being looked up, and a request with parts being planned. Running it against Llama 3.2 1B led to three fixes. Plain questions no longer offer tools, which the small model misused. Short capability questions are answered from the inventory. An answer that claims a change no tool made is now called out.
- Scanned PDFs in Knowledge. Pages without a text layer are read with text recognition, so scanned manuals, warranties, and price sheets become searchable and are cited by page. The first scanned PDF installs the recognizer (about 110 MB) on this computer; nothing is sent elsewhere. A scanned PDF attached to a chat explains how to connect it on the Knowledge page instead.
- Capability inventory. Yggdrasil keeps track of which models, computers, tools (built in, connected, and MCP), connected services, providers, and files exist right now, and what they let it do. Ask "Can you generate an image?" or "Which computer can run Qwen 2.5 14B?" and the answer comes from that inventory instead of a guess. Diagnostics lists every ability, with how it works or what would add it. Route: `/api/v1/capabilities`.
- Structured results. `/v1/chat/completions` supports `response_format` (`json_object` and `json_schema`). Answers are checked against the schema, safely repaired, and asked for once more if needed; JSON that still does not fit gets a 422 that lists the problems. Tool arguments are checked and repaired before a tool runs. Price and significance automations read their result's JSON with the same repairs, so `"$1,299"` counts as a price, and ask the model once when it is missing.
- Profiles & Orchestration. In advanced mode, a profile has an Orchestration section that sets its reasoning level and planning, as well as workers, parallelism, verification, tool calls, memory, context budget, fallback, and a time limit. Blank keeps each default.
- Run details. Every chat, API request, and automation is traced. In advanced mode, each answer has "Run details", showing the strategy, effort, models with their computer, load time, time to first token, tokens per second, and cached tokens, as well as tools with timings, plan workers, verification passes, retries, context size, and latency. Routes are `/api/v1/runs`.
- What left this computer. Settings lists every web search, page read, paired computer, external server, and connected service that a chat, automation, API request, or training run sent data to, with a 30-day summary. Memories and knowledge sources can be marked "This computer only"; chats that use them run here even when a paired computer would otherwise answer. Run records (stored prompts and tool results) are kept for 30 days by default, with a choice of 7 days to keeping them, and can be deleted at once. Routes are `/api/v1/egress` and `/api/v1/privacy`.
- MCP, both ways. **Tools → Add tools** adds MCP servers as tool sources: pick one from the gallery (Folders, Browser, Notion, Linear, Jira & Confluence, Sentry, GitHub, Context7, DeepWiki, SQLite, PostgreSQL, Brave Search, and more) and answer a question or two; paste any app's settings, a web address, or a command line; or bring the servers already set up in Claude Desktop, Claude Code, Cursor, VS Code, Windsurf, Gemini CLI, or LM Studio. Services with a sign-in open one in a browser window (OAuth with PKCE and app registration). Their tools work in chat and automations like connected services: reading runs, changes ask first, results are untrusted data, and secrets stay in the secrets directory and are scrubbed from results. Servers on this computer start when a tool is needed and stop when idle, get only a safe environment, and say plainly what to install when Node.js or uv is missing. Each source has a log, per-tool Ask first and on/off, ready-made prompts that start a chat, resources, and an opt-in for servers to ask the AI for help. Other apps can use Yggdrasil too: `/mcp` offers `ask_local_ai`, `list_local_models`, and `search_my_knowledge` within an API key's permissions, `yggctl mcp` bridges apps that start a program, and API Access gives the settings to copy for each app. Routes are under `/api/v1/mcp`. See [MCP](docs/mcp.md).
- The API gets the same assistant. `/v1/chat/completions` uses the whole conversation, including the system prompt, not only the last message. `reasoning_effort` sets the effort. An optional `yggdrasil` object opts into memory and connected knowledge, narrows tools, chooses effort and placement, and streams progress and tool activity. Answers carry their sources and steps. Each API key has permissions on the API Access page (memory, knowledge, tools, placement) that requests can narrow but never widen. API requests no longer use your memories unless they ask.
- Personalization. In Settings, choose answer length, tone, format, and units, and add a note about yourself and how you like answers. It applies to every chat, automation, and API request. It is kept apart from permissions: a preference or memory that tries to grant one ("you can always push without asking") is refused, with a pointer to Tool permissions.
- Knowledge search by meaning. Install an embedding model, such as Nomic Embed Text v1.5 (new in the catalog, 146 MB), and Mimir finds passages that answer a question even when they use different words: "warranty" finds your guarantee policy. Word matches still count, and passages both searches find rank first. Passages are embedded in the background and only again when their text changes; a chat, an automation, or training goes first. The Knowledge page says when a source is searchable by meaning. An installed reranker model reorders the best passages. Without an embedding model, search works as before.
- Connected services: GitHub and Home Assistant. Connect them in Settings with a token. The AI can then search and read issues and pull requests, comment (after asking), check lights and sensors, and control devices (after asking). Tokens stay on this computer outside the database, are never shown again or given to the AI, and are scrubbed from anything a service returns. Settings explains the narrowest access to grant. Routes are under `/api/v1/connectors`.
- Auto knows your specialized AIs. A question about what one was trained for (or one that names it) goes to that AI, and "What I did" says why. Questions that need the web, your files, or code still go to a general model, because specialized AIs answer without tools. An AI whose trained adapter is missing from this computer is skipped and explains itself.
- Embedding, reranker, and classifier models are recognized as supporting models. They are labeled on the Models page, and they are left out of the chat and automation model menus. Auto and fallback never pick them to answer.
- Sharing the computer. Chat comes first, then automations, then benchmarks, then training. An automation waits for your chat to finish instead of loading a model alongside it, a benchmark no longer unloads the model a chat is using, and training frees memory only once nothing else is running. Waiting work says what it is waiting for. A chat during training is still answered, with a note that training is using the computer and about how long is left. An automation that runs out of memory twice in a row is paused and tells you why, instead of failing every day.
- Notifications. The bell next to the Yggdrasil name shows unread notices: finished and failed automations, tools an automation skipped, finished or failed model downloads, deployed AIs, and newly paired computers. Click one to go to it, mark all read, or dismiss. Notices are kept by the daemon, so ones that arrived while the window was closed are waiting. Desktop notices are still posted for automations, and each delivery is recorded. Routes are under `/api/v1/notifications`.
- Automations use what chat uses: Auto can pick the model for each run, and memories and connected knowledge apply. An automation can notify only when it fails. Tools are approved when the automation is saved, including tools that change things, which the form lists apart. A run that reaches a tool you did not approve skips it, finishes, and tells you which tool to approve, instead of failing.
- Redrawn Yggdrasil mark: vector source, interface colors, small-size version, theme-aware favicon. The sources are in `docs/brand/logo/`, and `make icons` renders the Linux icons from them.
- Ratatoskr, the Yggdrasil mascot. He appears at the moments that matter: thinking while a reply is written, delivering work to a paired computer, celebrating a finished download or deploy, dropping his acorn on an error, and asleep when no model is loaded. He is a still frame when the system asks for reduced motion.
- Only the tools a request needs. A weather question is offered web search, not the terminal or Git; a request about a file or a commit gets file or Git tools. A tool that was not offered is refused, so the model cannot reach beyond it. Tool calls have time limits and report why they failed. `web.search`, `files.read`, `shell.run`, and similar capability names reach the built-in tools, and a call a small model writes out as text is still run.
- Effort in chat: Auto, Fast, Balanced, or Thorough. Fast answers in one go; Thorough reads more pages, uses the largest model that fits, and checks figures twice. Auto keeps quick questions fast and gives questions about your data, and requests with several parts, more care. The choice is remembered.
- Stop means stop. Stop ends every model call, tool, plan step, approval, and paired computer working on the reply, from any window or through `POST /api/v1/chat/stop`, and keeps what was already written, marked as stopped.
- Big requests are worked through in parts. "Compare Ollama, llama.cpp and MLX" looks each one up on the web side by side; "find three NAS drives, then compare price per TB, then make a spreadsheet" runs step by step, each step building on the last. A checklist shows the parts while they run, and one answer (or file) comes from all of them.
- Answers are checked before you see them. Calculations are recomputed, and figures in answers that use your files, knowledge, or web results must appear in the lines about the same thing. A wrong figure is sent back to the model to fix once. Anything still unconfirmed is called out under the answer ("could not confirm 20 in the sources"). An answer that only describes tools, instead of answering, is asked for again without tools.
- Small-model notes. On the Models page, models under 4B parameters say they can mix up facts and numbers from your files and knowledge, and suggest a larger model that fits the computer. In chat, an answer from a small model that used your files or knowledge carries the same note. Auto prefers a larger model for questions about your data.
- Files in and out of chat. Attach documents, spreadsheets, PDFs, and code files with the paperclip, by dropping them on the message box, or by pasting. The answer cites the file, and later questions in the chat can still use it, including files the assistant made ("add a column to that spreadsheet"). Ask for a file ("make a spreadsheet of these prices") and the answer comes with a download. Spreadsheets are real `.xlsx` workbooks. Files stay on this computer, under `artifacts/` in the data directory, and are deleted with their chat. Routes are under `/api/v1/artifacts`.
- Each page shows its Norse name above the title (Ratatoskr for Chat, Mimir for Knowledge, Bifrost for Computers, and so on), and each tab in the sidebar has its Elder Futhark rune. Tab names are unchanged. Click a Norse name, the mascot, or the logo for a short lore entry: who it is in the myths and what it is in Yggdrasil.
- Auto model. New chats use Auto, which picks an installed model for each message. Coding questions go to a coding model, questions about current information to a model that can use tools, and quick questions to a fast model that is already loaded when there is one. The answer's "What I did" says which model answered and why. `auto` is also a model in `/v1/models`.
- Current questions are looked up first. When a question needs current information (weather, news, prices, scores, links) and web search is allowed, Yggdrasil searches the web and reads the best page before the model answers, so small models answer from the page instead of guessing or picking the wrong tool.
- Quiet recovery. If a model fails before it answers, another installed model answers instead, and the answer says so, with a note when the model that answered is noticeably smaller. Auto skips a model that failed in the last 10 minutes.
- Memory. Say "Remember that…" in any chat, and Yggdrasil keeps it across chats, restarts, and model changes; "Forget…" and "What do you remember?" work too. Answers that used a memory list it as a source. The Memory page lists memories by category, where you can add, edit, pause, or delete them, and turn memory off. Each chat has a Memory on/off switch. Passwords, keys, and card numbers are not saved. Routes are under `/api/v1/memory`.
- Long conversations keep working. When a chat's history passes half of the model's window, older messages are summarized after the reply and the summary takes their place; the messages stay saved. The context meter shows how many messages the summary covers.
- Chat answers show their sources (web pages, knowledge passages, and files) and a "What I did" summary of searches, pages read, and knowledge used. Errors are explained in plain language with a next step and a retry; technical detail is under Details. Knowledge lookups show progress while the answer is prepared.
- Mimir connected knowledge. Connect a file, a folder, pasted content, an Excel workbook (each sheet is a table), or a PDF with a text layer (each page is cited); chat adds the passages that match each question. File and folder sources reindex when the files change. A profile lists `knowledge_sources`. Routes are under `/api/v1/knowledge`.
- Train your own AI. The Train page builds a specialized AI from a base model, examples, instructions, and connected knowledge. Yggdrasil recommends Training, Knowledge, or Both for each piece of material, flags weak examples, estimates training fit separately from inference fit, trains a LoRA or QLoRA adapter with MLX on Apple Silicon, and compares base and specialized answers before deployment. A deployed AI is the model `sai:<name>` in chat and `/v1/chat/completions`. The first training run installs a private Python environment under `runtimes/python` and downloads the base model's training weights from Hugging Face. Norn can train on a paired computer that has more memory, and the Review step lets you pick the computer; the adapter returns to the computer that owns the AI. The Train page and the Profiles editor attach existing knowledge sources, files, and folders. "Try an example" sets up a sample tire shop assistant with notes on each step, and the Material step shows sample files in each format.
- `yggctl completion <bash|zsh|fish>` prints a completion script for `yggctl` commands, `automations` subcommands, and their flags. Homebrew, the deb and rpm packages, and the macOS and Linux archives install or include the scripts.

### Changed

- Context budgets count tokens with the running model's own tokenizer. Which earlier messages fit, when a long conversation is summarized, and the context gauge's sections use llama-server's count instead of guessing four characters per token, a guess that is often well off for code and for languages other than English. Counts are cached for an hour and cleared with run records. When the model is not running on this computer, the estimate is used and the gauge still shows "~".
- Connected knowledge and retrieved content reach the model as labelled data in the user turn, not in the system prompt, and the model is told not to follow instructions inside it. After a turn reads untrusted content, a tool that changes something (write, terminal, Git commit or push) asks first even when the profile allows it. Knowledge search drops passages that score far below the best match, and table columns such as `in_stock` read as "in stock".

### Fixed

- The context gauge undercounted every turn after the first: it counted only the prompt tokens llama-server processed, not those it reused from its cache. It now shows the whole prompt.
- A daemon started with `--data-dir` stays in that directory when its `config.json` does not list `data_dir`. Before, it used the default data directory, so a second or test daemon could open your real database.
- The database refuses to start with two migrations of the same number, instead of silently skipping one.
- The chat page no longer reopens its event stream on almost every render, which dropped events such as a plan's checklist. The first message of a new chat no longer disappears while the reply is being written.
- A model whose `llama-server` exits while loading, for example a damaged file, now fails at once instead of after a two-minute wait.
- More current-information questions are recognized (news, scores, prices, exchange rates, "near me"), and cues match whole words only.
- Chat starts faster when a paired computer is offline. Peer health is checked in the background every 10 seconds and reused for 20; a check that has to run during a turn waits at most 1.5 seconds. Before, each turn waited the full timeout for an offline peer.
- A fast reply, such as a memory confirmation, no longer shows twice.

## [1.3.1] - 2026-09-29

Patch release. The API is unchanged. Binaries and the apt repository are not signed.

### Fixed

- A saved schedule turns on “Keep running in background,” so closing the window does not stop the daemon that runs it.

## [1.3.0] - 2026-09-29

Minor release. The API, CLI, and database migration are backward-compatible. Binaries and the apt repository are not signed.

### Added

- Scheduled automations. The daemon runs a saved prompt on a one-time, daily, weekly, or interval schedule, keeps a history of each occurrence, retries timeouts and connection failures, and can post an operating-system notice when the result matches the notification rule. Read-only tools can run while the window is closed. Automations are available in the desktop UI and through `yggctl automations`. `GET /api/v1/automations` is new.

## [1.2.1] - 2026-09-28

Patch release. The API is unchanged. Binaries and the apt repository are not signed.

### Added

- Branching and release strategy guide in `docs/development/branching-and-release-strategy.md`.

### Changed

- Flat logo in the web UI, favicons, home-screen icons, and Linux package icons.
- `make start` builds the UI and daemon and runs them. `make help` lists targets.

## [1.2.0] - 2026-09-28

First stable release. It follows `v1.2.0-beta.3`. Binaries and the apt repository are not signed.

### Added

- Unsigned Windows amd64 headless archive on the GitHub Release.
- Homebrew formula written and merged from the release workflow.
- Coverage badge, golangci-lint, and ESLint in CI.
- Repository guides for contributors, security reports, privacy, architecture, compatibility, and issue forms.
- Feature specifications and the distributed-inference research brief. Those documents are plans, not shipped behavior.

### Changed

- Web UI uses Tailwind 4.
- The README leads with the local-AI goal, the demo, and packaged install paths.

### Fixed

- llama.cpp health errors include the install location when the runtime binary is missing.

## Earlier releases

Notes checked into `docs/releases/` for versions that have them:

- [1.0.0-beta.1](docs/releases/1.0.0-beta.1.md)
- [0.1.0-beta.1](docs/releases/0.1.0-beta.1.md)
- [0.1.0-alpha.28](docs/releases/0.1.0-alpha.28.md)
- [0.1.0-alpha.27](docs/releases/0.1.0-alpha.27.md)
- [0.1.0-alpha.26](docs/releases/0.1.0-alpha.26.md)
- [0.1.0-alpha.25](docs/releases/0.1.0-alpha.25.md)
- [0.1.0-alpha.24](docs/releases/0.1.0-alpha.24.md)
- [0.1.0-alpha.23](docs/releases/0.1.0-alpha.23.md)
- [0.1.0-alpha.22](docs/releases/0.1.0-alpha.22.md)
- [0.1.0-alpha.21](docs/releases/0.1.0-alpha.21.md)

Tags also exist for the 1.1 and 1.2 beta lines. Their user-guide snapshots are under `docs/`. Published release bodies are on [GitHub Releases](https://github.com/yeixio/yggdrasil-core/releases). This changelog does not invent entries for those tags.
