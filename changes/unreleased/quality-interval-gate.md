### Changed

- The quality sets fail only when their pass rate's interval says so: the
  whole 95% interval under the bar, or, given a baseline run with
  `TOSKAR_QUALITY_BASELINE`, a significant drop from it (p < 0.05). A rate
  just under the bar but within run-to-run noise passes with a note (#834).
