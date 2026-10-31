### Fixed

- A model bigger than the graphics card runs, partly on the processor,
  instead of stopping while it loads. Toskar no longer asks llama-server to
  put every layer on the card; llama-server fits as many as the card's free
  memory holds, a draft's too, and keeps the rest in system memory, a
  mixture-of-experts model's experts first. An older llama-server without
  this still gets every layer on the card (#955).
- With a draft model, Performance showed the draft's layers on the
  graphics card instead of the chat model's (#955).
