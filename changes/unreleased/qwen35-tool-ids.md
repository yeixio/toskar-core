### Fixed

- Qwen 3.5 models use tools. They put their own label for a call in its id, such as `terminal_1` or `run_test_001`, so every call was refused and they answered without tools. Toskar now reads such a call as the tool it names, or the one tool its arguments fit, and asks again by the tools' ids when it can't tell. On the long tasks, Qwen 3.5 4B went from none done to 2 of 3 (#1124).
