### Fixed

- On Windows with an NVIDIA card, where llama.cpp names the card with its
  PCI address, Performance and the model's GPU status showed the name with
  part of the address, such as "RTX 3080 Ti Laptop GPU) (0000:01:00.0" (#955).
