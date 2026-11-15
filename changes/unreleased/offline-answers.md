### Fixed

- Offline, a question that needs the internet, such as today's AI news
  or a web search, is answered with saying this computer isn't
  connected. Before, the model wrote an answer from memory as if it had
  looked: asked for the news, it made up the day's stories (#965).
- Pages and setup steps that need the internet say this computer is
  offline, and which site couldn't be reached, instead of showing
  `dial tcp: lookup huggingface.co: no such host` or sending you to
  Diagnostics. API errors for it have the code `OFFLINE` (contract
  1.92).
