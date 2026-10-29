### Fixed

- A model's fit on the Models page now says how well it runs. A model that
  needs more than the graphics card's own memory runs partly on the
  processor, so it's tight at best and says so, where a 32B model was an
  excellent fit for an 8 GB card. On a computer without unified memory,
  some memory is kept for the operating system, so a 9B model is no longer
  a good fit with 8 GB. Best overall, Best quality, and the other badges go
  to models that fit well, and Best overall is the model setup recommends
  (#932).
