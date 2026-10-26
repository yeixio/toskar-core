### Fixed

- A model loading for a request is no longer unloaded as idle partway
  through. The idle unload went by the model's last use, which was the
  previous instance's, often long ago. So a model that had been unloaded
  once could be stopped again while it loaded, and the request moved to
  another model or waited a long time. A load in progress now counts as
  use, and the model stays loaded until it finishes.
