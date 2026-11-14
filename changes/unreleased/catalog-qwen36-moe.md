### Added

- Qwen 3.6 35B-A3B in the model catalog: a mixture of experts that uses
  3B of its 35B parameters for each word, and sees pictures.

### Changed

- A mixture-of-experts model too big for the graphics card is labelled by
  the computer's memory, with its experts running from it and the rest on
  the card, instead of Tight like a dense model of its size.
