### Fixed

- gpt-oss can use tools and do tasks. Its calls come back from llama.cpp only when the request offers the tools itself, so a chat with a folder, a web search, or any other tool stopped before doing anything. Toskar now offers them that way to models in gpt-oss's chat format, and reads the call as any other (#1099).
