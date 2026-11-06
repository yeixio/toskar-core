### Fixed

- A model added from a file can use tools when its chat template takes them, as gpt-oss, Qwen, and Llama do. It was treated as unable to call any, so its answers to ordinary questions, such as one about a file in the chat's folder, could only guess. Models added before this are read from their file, with nothing to add again (#1101).
- A question that uses all its tool calls is answered from what they found, instead of "I stopped after 3 tool calls so this request would not loop".
