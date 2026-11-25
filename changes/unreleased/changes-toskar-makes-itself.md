### Fixed

- Turning off Keep running in background now sticks. Toskar turns it on
  when you save your first schedule, and after that leaves it as you set
  it; the Automations page says when schedules won't run with the window
  closed (#1209).
- The files an automation makes are kept as long as its runs, 30 days by
  default, instead of a week (#1209).

### Security

- The llama.cpp download is checked against the checksum GitHub publishes
  for it before it replaces anything (#1209).
- Tools from an MCP server added by web address ask before they run until
  you allow them, even ones the server says only read (#1209).
