### Fixed

- On Linux, a running model's measured memory is what its process
  allocated. It counted the model's file too, which stays mapped after the
  weights go to the graphics card, so the same model read anywhere from
  2.8 GB to 9.3 GB.
