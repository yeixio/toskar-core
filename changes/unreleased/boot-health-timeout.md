### Fixed

- The boot screen no longer waits for good on a health check that never
  answers: each check gives up after six seconds and is tried again, and
  **Try again** starts a new one (yeixio/toskar-apps#461).
