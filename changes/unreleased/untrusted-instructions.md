### Security

- Text on a web page, in a file, in a Knowledge passage, or in a tool's
  result that gives orders to the AI is left out before the model reads
  it: forged chat turns written in a model's markers (`<|im_start|>`,
  `[INST]`, and the like), and sentences such as "ignore previous
  instructions" or "note for AI assistants". A note stands where the text
  was. Labelling that text as data wasn't enough: a 7B model followed a
  page's instruction to end its answer with a word every time, and now
  never does. The sentence filter is English only, so a tool that changes
  something still asks first after an answer reads untrusted text (#938).
