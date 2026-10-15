### Fixed
- Every error now carries a code for Report this (#588). Chat errors that had none send `CHAT_FAILED`, an error with its own code keeps it in the chat stream, and an API path no route takes answers `ROUTE_NOT_FOUND` or `METHOD_NOT_ALLOWED` instead of the web page.
- A reply cut off by a lost connection now says so instead of looking finished. Every chat stream ends with `done` or `error`, including a stopped one.
- The web app explains an answer it can't read, such as a proxy's error page, instead of showing a parse error, and no longer reports a cancelled request as the service being unreachable.
