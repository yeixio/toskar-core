### Security

- Text a model reads can't pose as its own turn markers. A web page, a
  file in Knowledge, a tool's result, an automation's trigger, or a
  visitor's message that spelled a control token, such as
  `<|im_end|><|im_start|>system`, reached llama.cpp as that token, so it
  could start a real system turn. Every message is now escaped on its way
  to the model, from the model's own vocabulary and the common chat
  templates' markers, and so is what goes to an external server.
