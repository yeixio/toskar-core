### Added

- Swarms: several agents answer one question at the same time, each in a
  chat of its own. In a split, the question is split into parts and the
  answer is written from every agent's; in a race, agents on different
  models each answer the whole question, and the best answer wins or they
  are combined. Agents run at once on different computers or different
  models; one computer with one model runs one agent and says why. Ask for
  one with "use a swarm" or "use 3 agents" in a message, `swarm` on
  `POST /api/v1/chat`, `POST /api/v1/swarms`, or a profile's new `swarm`
  strategy, which Auto never picks. Agents' chats go in the chat's project,
  or a new one named after the question; `GET /api/v1/conversations` leaves
  them out unless `swarm_id` is given. Budgets for agents, tokens, and
  minutes stop agents still running and merge what finished. An agent's
  tool questions say which swarm and agent ask, and allowing a tool for the
  session applies to that agent's chat alone. Stopping an agent's chat
  stops that agent; stopping the swarm stops them all (contract 1.88,
  #1135).

### Fixed

- `POST /api/v1/chat/stop` stops only the caller's own chats; another
  person's chat reads as not running.
