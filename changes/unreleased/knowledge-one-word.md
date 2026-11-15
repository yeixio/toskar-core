### Fixed

- Knowledge no longer brings a passage into a chat because it shares one
  everyday word with a longer question. Asked about coffee shops near
  the harbor, a refund policy for "Harbor Supply" came along, and a
  small model said it had no information instead of looking it up. A
  shared code or size, such as a SKU, is still enough on its own, and
  passages that turn out to be about something else are set aside in
  the answer (#1217).
