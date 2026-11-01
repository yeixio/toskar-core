### Fixed

- On Windows with an NVIDIA card, where llama.cpp names the card with its
  PCI address, Performance and the model's GPU status showed the name with
  part of the address, such as "RTX 3080 Ti Laptop GPU) (0000:01:00.0" (#955).
- On Linux, an AMD or Intel card's name was lspci's whole line, such as
  "03:00.0 VGA compatible controller: Advanced Micro Devices, Inc. [AMD/ATI]
  Navi 31 [Radeon RX 7900 XT/7900 XTX] (rev c8)". It's now the name people
  know, such as "AMD Radeon RX 7900 XT/7900 XTX" (#955).
