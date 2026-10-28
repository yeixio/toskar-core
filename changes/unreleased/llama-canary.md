### Added

- A nightly llama.cpp canary checks that the newest release, the one Toskar
  installs, computes what the known-good tag does: Metal and the CPU on a
  Mac, Vulkan and the CPU on Linux. It compares next-token probabilities
  over a fixed text (KL divergence, top-token agreement, perplexity) and
  opens an issue with the numbers when a release drifts (#833).
