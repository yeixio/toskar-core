# What people may use

Admins choose what each role, and each person, may use on their Toskar (#589). For example, Visitors chat but can't upload files, Members can't make video, or a child's account has no web lookup.

Roles decide which pages and settings someone reaches ([API](api.md)). Capabilities decide what they may do within them.

## Roles and people

Each role has capabilities. These are the defaults:

| Capability | Owner | Admin | Member | Visitor |
| --- | --- | --- | --- | --- |
| Files | ✓ | ✓ | ✓ | |
| Pictures in | ✓ | ✓ | ✓ | ✓ |
| Making pictures | ✓ | ✓ | ✓ | |
| Making video | ✓ | ✓ | ✓ | |
| Voice | ✓ | ✓ | ✓ | ✓ |
| Web | ✓ | ✓ | ✓ | ✓ |
| Knowledge in answers | ✓ | ✓ | ✓ | ✓ |
| Adding to Knowledge | ✓ | ✓ | ✓ | |
| Automations | ✓ | ✓ | ✓ | |
| Tools that change things | ✓ | ✓ | | |
| Choosing a model | ✓ | ✓ | ✓ | ✓ |
| Away from home | ✓ | ✓ | ✓ | ✓ |

- **Where:** Administer → People. **What people may use** has a switch for each capability and role, with a dot where it differs from the default. Under each person, **What they may use** sets their Allow or Block.
- **Changing a role:** an Admin changes what Members and Visitors may use. Only the Owner changes what Admins may use. The Owner always has everything, so nobody can lock them out.
- **One person:** each capability can be **Allow** or **Block** for one person, in place of their role's. Anything not set is as their role's.
- **Portal guests:** a [chat portal](api.md#chat-portals)'s guests have the Visitor's capabilities, and the portal's own tool setting narrows them further.

Their API keys and paired devices act as them, so the same applies through `/v1`, MCP, and the phone. The app hides what's off: the attach button offers only the files someone may send (or is gone), the model picker is gone without **Choosing a model**, read aloud without **Voice**, and **New automation** and its ideas without **Automations**.

## Each capability

- **Files** (`files`):
  - attaching and uploading documents, spreadsheets, PDFs, and code;
  - the model's tools that read, search, or make files.
- **Pictures in** (`pictures_in`): sending pictures and videos for a model to look at. A picture or video can be uploaded with this alone.
- **Making pictures** (`make_pictures`): making and editing pictures. Asking for one anyway gets a plain answer, such as "Making pictures is turned off for your account. Ask Sam or another Admin to turn it on."
- **Making video** (`make_video`): making clips, which ties up the graphics card for minutes. Asking for one anyway gets the same kind of answer.
- **Voice** (`voice`): speaking a message (transcription) and having answers read aloud.
- **Web** (`web`): web lookup, reading pages, places and maps, and the browser.
- **Knowledge in answers** (`knowledge`): Knowledge sources in answers. Without it, a profile's Knowledge is left out of their chats.
- **Adding to Knowledge** (`knowledge_add`): adding, changing, refreshing, and removing Knowledge sources.
- **Automations** (`automations`): making automations, and the model offering to schedule one.
- **Tools that change things** (`changing_tools`): commands, writing files, Git commits and pushes, and MCP and plugin actions that change things. Without it, those tools aren't offered to the model at all.
- **Choosing a model** (`choose_model`): picking a model by hand. Without it, every chat uses Auto.
- **Away from home** (`remote_access`): using paired devices away from home, through access from anywhere.

## The API

- **Each role:** `GET /api/v1/roles/capabilities` answers every capability in order, each role's, and the defaults. `PUT /api/v1/roles/{role}/capabilities` with `{"make_video": false}` changes a role's.
- **One person:** `GET /api/v1/people/{id}/capabilities` answers `set`, their own Allows (`true`) and Blocks (`false`), and `effective`, what they may use now. `PUT` with `{"web": false, "files": null}` changes them, where `null` is as their role's.
- **Who's asking:** `GET /api/v1/me` answers `capabilities`, so an app hides what's off.
- **Refusals:** something turned off answers `403 CAPABILITY_OFF`, with `details.capability` and `details.admin`, the Owner's name, to say who can turn it on.
- **Unknown capability or the Owner's:** `400 CAPABILITY_UNKNOWN` for a capability Toskar doesn't know, and `400 OWNER_FIXED` for the Owner's.
