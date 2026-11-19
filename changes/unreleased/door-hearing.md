### Fixed

- Talk with the visitor now hears someone speaking at a Reolink doorbell.
  The doorbell's microphone is quiet, and Toskar listened only for speech
  about twice as loud as a visitor's voice, so it greeted them and then
  ended the visit as if nobody had answered.
- What Toskar says through a Reolink doorbell is louder: the voice is
  raised to near full volume before it's sent, since the doorbell's small
  speaker made it hard to hear. It also leaves out the low end the small
  speaker can't play and evens out quiet and loud syllables, so it sounds
  about 7 dB louder at the same peak.
- The doorbell greets a visitor sooner: its voice, and its listening, are
  readied while Toskar looks at the camera and writes the greeting, rather
  than started after.
- A visit's notification is plain sentences, without headings, lists, or
  notes about Toskar's own settings, and the doorbell no longer calls a
  visitor by a name they only mentioned, such as who sent a package.
- After the doorbell says goodbye, it keeps listening for a minute in case
  the visitor has more to say, instead of ending the visit at once.
- With Talk with the visitor on, the doorbell greets someone the moment
  it sees them ("Hello! How can I help you?", in the automation's
  language), instead of first looking at the picture and writing a
  greeting, which took about 20 seconds. Someone passing by hears the
  greeting too.
