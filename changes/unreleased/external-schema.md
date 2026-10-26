### Fixed

- Structured output holds on an external OpenAI-compatible server and on a
  paired computer too. The JSON Schema reached only this computer's
  llama.cpp, so a model elsewhere could answer outside it, and the request
  failed with `422 INVALID_STRUCTURED_OUTPUT`. An external server now gets
  it as `response_format` (and is asked again without it if it refuses),
  and a paired computer's llama.cpp constrains the reply as this one's does.
