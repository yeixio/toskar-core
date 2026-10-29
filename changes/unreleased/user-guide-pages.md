### Changed

- The user guide holds detailed pages. A section can add a summary, a
  kind (task, concept, or reference), and blocks: subheadings, steps,
  "What you need" lists, tips and warnings, tables, screenshots, code
  examples, and related sections. `scripts/check-user-guide.py` holds a
  detailed section to the bar in `docs/user-guide/STYLE.md`: a screenshot
  or a tested example, plain sentences, and no device brand names. A pull
  request that adds a feature changes the guide too, or has the
  `no-user-docs` label saying why (#882).
- Install and start your first chat says what each amount of memory runs,
  adds the apt repository, and shows the download finishing and a first
  answer. Run without a desktop is a full page: the service, opening
  Toskar from your own computer through SSH, being the Owner, updating,
  and troubleshooting. The install script and apt lines on the page are
  the ones CI runs on a fresh Ubuntu computer: an example can name the
  test that runs it with `runs_in` (#885).
- Chat covers what each model can read when you attach a picture, answers
  from the web and what they need, and which version of an answer a chat
  continues from, with a generated screenshot of an attached spreadsheet
  read, cited, and answered with a made spreadsheet (#886).
- Choose and manage models explains what each kind of model is for,
  community ratings and exactly what sharing one sends, new models in the
  News, that a model needing more than the graphics card is tight, and
  `toskarctl models`. New generated screenshots show Browse all models with
  a download under way and Add a model › From another app (#887).
