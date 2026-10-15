### Added

- Admins choose what each role, and each person, may use (#589):
  - the capabilities are files, pictures in, making pictures, making video,
    voice, the web, Knowledge, adding to Knowledge, automations, tools that
    change things, choosing a model, and access from anywhere;
  - Visitors start without files, making pictures or video, or
    automations, and Members without tools that change things;
  - Toskar enforces them everywhere a person comes in, and `GET
    /api/v1/me` lists them so apps can hide what's off;
  - asking for something turned off gets a plain answer saying who can
    turn it on. See [What people may use](docs/people.md).
