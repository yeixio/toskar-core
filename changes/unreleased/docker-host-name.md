### Fixed

- An app in Docker Desktop or Podman on the same computer, such as Open
  WebUI, n8n, or Home Assistant, reaches Toskar at host.docker.internal
  without an API key. Before, Toskar turned that name away as if a website
  were using it, and Open WebUI showed an empty model list. (#1057)
