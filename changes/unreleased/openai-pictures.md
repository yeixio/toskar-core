### Fixed

- Pictures sent to `/v1/chat/completions` reach the model. They were read,
  then dropped, so a model answered without having seen them. They're now
  shown to a model that can see, as pictures attached in the chat are, and
  with none, the answer says so. A picture is a `data:` URL, or an
  `http(s)` one Toskar fetches from the internet only.
