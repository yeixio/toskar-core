### Changed

- A reasoning model can keep its thinking between tool calls in one answer,
  instead of working out its plan again after each call. It's off until
  the quality set shows it helps; `TOSKAR_KEEP_REASONING=field` or `inline`
  turns it on to compare.
