### Fixed

- Answers from `/v1/chat/completions` look like OpenAI's to the libraries
  and tools that read them. Each has its own `id` and a `created` time,
  `usage` counts the tokens (cached and reasoning ones too, and a stream
  sends it with `stream_options.include_usage`), and `finish_reason` says
  `length` when an answer was cut short at its token limit, instead of
  always `stop`. Errors use OpenAI's shape, `type`, `param`, and `code`,
  including an error partway through a stream.
