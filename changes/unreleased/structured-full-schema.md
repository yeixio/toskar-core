### Fixed

- Structured output uses the whole JSON Schema a request sends. Only
  `type`, `properties`, `required`, `enum`, and `items` reached the model's
  grammar and the check, so `anyOf`, `$ref`, `additionalProperties: false`,
  bounds, `pattern`, and `const` were dropped without notice, and a schema
  whose top level was `anyOf` accepted anything. The schema now goes to
  llama.cpp as sent, answers are checked against every keyword, and a
  schema that isn't valid JSON Schema, refers outside itself, or is over
  64 KB is refused with `400 INVALID_RESPONSE_FORMAT`.
