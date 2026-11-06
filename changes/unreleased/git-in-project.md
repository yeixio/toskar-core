### Fixed

- In a chat with a folder, or a task in the terminal, Git works in that folder, as the file tools and commands do. It used Toskar's own workspace, so a commit could land in the wrong place. Git's view of the project no longer counts as outside content either, so a task that checks `git status` first doesn't ask before every later step (#1110).
