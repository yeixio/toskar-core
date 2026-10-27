### Changed

- The user guide holds detailed pages. A section can add a summary, a
  kind (task, concept, or reference), and blocks: subheadings, steps,
  "What you need" lists, tips and warnings, tables, screenshots, code
  examples, and related sections. `scripts/check-user-guide.py` holds a
  detailed section to the bar in `docs/user-guide/STYLE.md`: a screenshot
  or a tested example, plain sentences, and no device brand names. A pull
  request that adds a feature changes the guide too, or has the
  `no-user-docs` label saying why (#882).
