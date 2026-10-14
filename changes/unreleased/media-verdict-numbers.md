### Changed

- When a picture or video model may be slow or tight, the Tools page and
  the chat's setup offer say what it needs and what the computer has, such
  as "Needs about 16 GB of graphics memory; this computer's graphics card
  has 10 GB" (#550).
- On Apple silicon, models are checked against the share of memory the
  graphics may use, about two thirds, or three quarters above 36 GB, so a
  model that needs more is marked as possibly slow before it's set up.
- The chat's setup offer carries the model's verdict and these numbers
  (`verdict`, `short`, `need_bytes`, `have_bytes`, contract 1.34), and
  `GET /api/v1/images/setup` and `/video/setup` add them to each model,
  with the computer's `graphics_bytes` and `shared_graphics`.
