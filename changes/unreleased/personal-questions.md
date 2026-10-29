### Fixed

- A question about your own life, such as "Where does my sister live?",
  could be searched on the web when the model first said it didn't know,
  and answered from pages that had nothing to do with you. Toskar now asks
  the model once more to answer from the chat and its summary, and to say
  it doesn't know if it isn't there. Such a question is never looked up
  (#834).
- When a long chat was summarized a second time, the new summary could
  cover only what was said since the first one, and lose everything before
  it. The summary now keeps the earlier summary's facts (#834).
