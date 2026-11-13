### Changed

- The quality run measures swarms against a single agent (#1135): six
  questions in several parts, answered from the set's fixed web pages, and
  four tasks in a Git folder whose parts are in files of their own, scored
  by the folder's tests. Each is asked of one agent on each model and of a
  swarm on the same models (split, and race with two or more), and the
  report counts what was done, the time, the tokens, how many agents ran at
  once and on how many computers, and why a swarm ran as one agent, with
  the swarm against one agent side by side. The set answers yes to writes
  in its own folders and to applying its swarms' changes there, and no to
  everything else. The weekly run measures the recommended models with two
  slots, and the Quality workflow's `sets`, `swarm_models`, and
  `swarm_slots` inputs run it by hand.
