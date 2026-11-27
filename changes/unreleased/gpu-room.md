### Fixed

- Answers no longer slow to a crawl when other models fill the graphics card.
  Before a model starts, Toskar unloads the idle models holding the card,
  least recently used first, so the model that answers runs on the card
  rather than on the processor. Models set to **Always running**, and models
  writing an answer, stay loaded.
- Add a model › From another app no longer offers the parts of an
  image or video model, such as FLUX.2's Qwen3-4B text encoder, as chat
  models.
