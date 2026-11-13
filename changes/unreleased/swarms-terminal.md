### Added

- Swarms in the terminal: `toskarctl chat --swarm` sends each message to a
  swarm (`split`, the default, or `race`), and `/swarm` sends the next one,
  with `/swarm off` to stop. While the agents work, each has one line,
  updated in place: its part, computer, model, how it's doing, and its
  latest step; then the merge, any change that couldn't be merged, and for
  a task the branch, the checks, and whether the change is in your folder,
  then the answer. Piped, each change is a plain line on standard error.
  An agent's tool questions show and are answered in the terminal, labeled
  with the agent, and "always" applies to that agent alone. `/agents` lists
  the last swarm's agents, `/open N` continues an agent's chat, and `/back`
  returns. Ctrl-C stops the whole swarm (#1135).
