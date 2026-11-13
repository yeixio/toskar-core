### Added

- Swarms for tasks in a folder: when a task in a chat's folder asks for a
  swarm, the coordinator looks at the folder, read-only, and splits the task
  into parts with the files each expects to change; parts that change the
  same files go to one agent. In a Git repository each agent works as a task
  in a worktree of its own, on a branch of its own, starting from what you
  see, uncommitted changes and new files included, and its work is
  committed there as Toskar. The branches are merged one at a time; a
  conflict goes to a fix-up agent, and one it can't settle is reported with
  the agent's branch. The project's checks run on the merge, and the result
  is applied to your folder as uncommitted changes once you allow it. A
  folder that changed since the swarm started, so the change no longer
  applies, isn't forced: the answer names the branch to merge. In a race,
  an agent whose checks fail can't win over one whose checks pass, and the
  judge picks among the rest. Your branch, HEAD, index, and stash are never
  touched. Worktrees are removed when the swarm ends; branches are kept for
  7 days, or until `DELETE /api/v1/swarms/{id}` deletes them. In a folder
  that isn't in Git, the agents take turns in the folder itself, and the
  answer says so. Swarms gain `folder`, `branch`, `checks`, `conflicts`, and
  `applied`, and their agents `paths`, `branch`, and `checks` (contract
  1.88, #1135).
