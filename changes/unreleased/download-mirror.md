### Added

- Model downloads can go through a download cache on your own network:
  set `TOSKAR_DOWNLOAD_MIRROR` to its address, and Toskar asks it before
  Hugging Face, falling back to Hugging Face when the cache fails or its
  copy doesn't match the model's checksum.
