### Added

- A step-by-step guide for Claude Code and Codex CLI with Toskar as their model provider, on this computer and from another one with a key, tested with Qwen 3.5 9B (`docs/integrations/coding-agents.md`, #1060), linked from the user guide's Terminal page.

### Fixed

- Claude Code and Codex work with Qwen 3.5. Toskar passed their system messages on where they were sent, partway through the conversation, and Qwen 3.5's chat template refused them, so every request failed with "System message must be at the beginning". They now reach the model as one system message at the start (#1060).
