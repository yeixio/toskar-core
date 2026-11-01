### Changed

- Context windows past 32K tokens use an 8-bit key/value cache, about half
  the memory, which answered as well in the quality set. Choosing 64K or
  128K for a model, or an API request that needs it, now fits more
  computers, and the memory estimate counts the smaller cache (#962).
