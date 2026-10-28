### Added

- A nightly llama.cpp canary checks that the newest release, the one Toskar
  installs, computes what the known-good tag does: Metal and the CPU on a
  Mac, Vulkan and the CPU on Linux. It compares next-token probabilities
  over a fixed text (KL divergence, top-token agreement, perplexity) and
  opens an issue with the numbers when a release drifts (#833).
- On the Mac, the canary also checks a model of each family that thinks
  before it answers (Qwen 3, DeepSeek R1's distills, gpt-oss): the thinking
  comes apart from the answer, and turns off when asked (#840).
