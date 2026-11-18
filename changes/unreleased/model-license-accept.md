### Added

- A model whose license has terms to accept, such as Gemma or Llama, shows
  them before it's installed from Models: read them, then Accept and install
  (#593). Toskar keeps a record of who accepted which license and when, for
  the Admin (contract 1.103: `accept_license` on install, `license.accepted_at`,
  and `GET /api/v1/models/license-acceptances`).
