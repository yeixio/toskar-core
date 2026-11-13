### Fixed

- A task, or a swarm agent's part, that names a file, such as "Write
  Reverse in strs/strs.go", changes that file in its folder instead of
  attaching a new file to the chat. Ordinary chats with no folder still make
  the file (#1135).
- A task swarm's change whose project checks failed, or ran out of time,
  always asks before it's applied to your folder, whatever your profile
  allows. The question says the checks failed, and the answer says whether
  the change was applied because you chose to or wasn't, with its branch
  (#1135).
- In the terminal or a chat with a folder, a request to fix or change code
  later in the message, such as "The test in sum_test.go fails. Fix Sum so
  it passes.", is done as a task instead of answered as a question. "Why
  does my test fail?" is still a question (#1135).
