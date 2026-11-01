### Added

- A coding agent with many tools, such as one with MCP servers, works with a
  local model. When a raw request's tools take more than a quarter of the
  model's window, the model gets the most relevant ones and a
  `tools_search` tool to find the rest; Toskar answers it, adds what it
  found, and asks again, so the agent sees only calls to its own tools. A
  long tool list no longer makes a request too long for the model (#843).
