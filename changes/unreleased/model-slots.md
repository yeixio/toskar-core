### Added

- Agents at once, per model: with Advanced mode on, a model's More actions
  menu on Models sets how many of a swarm's agents run on it at the same
  time, 1 to 4 (1 by default). llama-server starts with `--parallel` set to
  it and a context of the model's window times that number, so each agent
  keeps the whole window; the model uses more memory, and its fit and Auto's
  window count it. A number that leaves an agent less than 8K of context
  (`MODEL_SLOTS_CONTEXT`), or that doesn't fit the computer
  (`MODEL_SLOTS_MEMORY`), is refused. A loaded model reloads with it on its
  next request. Swarms run that many agents on the model at once, and the
  rest wait; a paired computer's models count as it's set there.
  `PUT /api/v1/models/{id}/slots` sets it, and each chat model installed on
  the computer says `slots` (contract 1.88, #1135).
