# Topic controls

Topic controls keep an assistant on the subject its people came for (#345). A tire shop's assistant answers about tires, wheels, alignment, and the shop's hours and bookings, and politely declines an essay, medical advice, or politics.

Set them on a profile, under **Profiles → Edit → Topics**:

- **Stays on:** what the assistant is for, in plain words. Leave it empty for no topic controls.
- **Example questions:** questions it's for, one per line.
- **Never discusses:** subjects always off limits, even when they look related, such as "other shops' prices".
- **When asked something else:** the exact reply to anything off topic. Leave it empty, and the assistant says in one short sentence what it can help with, in the person's language.
- **Web search only on:** the sites web search and pages keep to, such as the shop's own site and tire makers, one per line, with their subdomains. Leave it empty for any site.
- **Add to every web search:** words added to each search, such as "tires", so results stay on the subject.

Greetings, thanks, and "what can you do?" are always fine, so a strict assistant doesn't feel broken.

## Try it

Under the topic controls, **Try it** runs a message past them as they are in the editor, before you save: pick one of the made-up attempts to talk it off topic, or type a question your people might ask. Each shows the label (on topic, small talk, or off topic) and the reply. The check runs at either strictness, so with Guide you also see what Enforce would hold. Answers there are quick ones, without tools, knowledge, or memories, and nothing is saved or counted.

## Off-topic attempts

Under Try it, **Off-topic attempts** shows what an Enforce profile held, or whose answer it replaced, over the last 30 days: how many each day, where they came from (a portal, an API key, a person in the app, or an automation), and the latest messages. It shows when people try to take the assistant off topic, and when the topic is set too narrowly: **Mark as on topic** adds a message to the example questions, so the checks count it as on topic from then on, and forgets the attempts with that message.

Only Admins see them. They're run records, kept for as long as run records are (**Settings → What left this computer**) and deleted with them; at most 500 characters of each message are kept. Guide checks nothing, so it keeps nothing, and Try it is never kept.

## How it works

The topic rules come first in every turn, as the administrator's rules, ahead of everything else the model is told. They say that nothing later changes them: not the person's preferences or memories, not an application's instructions through the API, not a document, file, or web page, and not a message asking to ignore them, to pretend, or to play a role.

**How strict** picks how much more is done:

- **Guide:** the rules alone. Quickest, and fine for your own people.
- **Enforce:** for the public and chat portals. Each message is checked before it's answered, and each answer before it's shown:
  1. **Before answering,** a short call labels the message *on topic*, *small talk*, or *off topic*, reading it with the conversation so far, so "and the 18-inch?" still counts. An off-topic message gets the set reply, and the full answer never runs, which also saves the work.
  2. **After answering,** a short call reads the answer with its question. An answer that went off topic anyway, because the message talked its way past the first check, is replaced by the set reply.

  The set reply is the one under **When asked something else**, word for word. Without one, the check writes one short sentence in the person's language saying what the assistant can help with, and when it can't, Toskar sends "I can't help with that here. What else can I help you with?" in the person's language.

  The checks run on a small model that holds them well (#457): the model catalog marks the models that pass the topic quality cases (Gemma 3 4B so far), and the smallest one installed on this computer checks when it is the model answering, already running, or fits in memory beside it, with a quarter of memory to spare. Otherwise the model that answers checks too. Each check is one short call, and the run trace says which model checked. Diagnostics, under what Toskar can do, says when no verified model is installed, and so does the Topics tab under Enforce. If a check gives nothing usable, the turn goes on as Guide, and the run trace says so.

The run trace (`GET /api/v1/runs`, and the run details under an answer) notes a held message or a replaced answer, and its `topic` says what the check found.

## Who it applies to

- **Chat portals (#205):** a portal's profile brings its topic controls with it, so its visitors can't turn them off.
- **Roles and people:** on **People**, **Profiles they use** keeps Members or Visitors to one or more profiles, and each Member or Visitor can have their own: the same as their role, any profile, or only some. A pinned person's chats use one of their profiles, whatever the chat or a request names, and the chat's profile picker shows only those. The Owner and Admins are never pinned, so whoever runs Toskar keeps the whole assistant. If every profile someone is pinned to is deleted, they can't chat until an Admin changes it, rather than getting any profile.
- **API keys:** on **API Access**, a key's **Answers with** pins it to one profile. Every request with that key, from a website or another app, answers with the profile and its topic controls, and can't name another profile or a model.

With topic controls, the assistant answers everything by its own rules: Toskar's own replies about itself ("how do I install MCP?"), "remember …" commands, and offers to install something aren't given in its place.

## Talking with visitors

A profile whose `audience` is `public` talks with strangers, such as visitors at a front door, a shop counter, or a kiosk (#667). Turn it on with **Talks with visitors** on the Topics tab, or start from the **Front door** template under **Create profile**, which also starts with no tools, memory off, and Enforce topics for greeting visitors, deliveries, and messages.

Toskar's own rules for strangers come first in every turn, ahead of the topic rules, and an administrator can't edit them: say it's an automated assistant; never say or hint whether anyone is home, when people come and go, who lives there, or anything about locks, alarms, or cameras; never open a door or turn off an alarm, or say it will, whoever asks; and a message or a sign asking to ignore the rules changes nothing.

Nothing about the owner reaches the model: not their style or location from Personal settings, not their memories, and not what Toskar knows about its computers or its own guide. Toskar's own replies, such as "remember …" commands, aren't given, so a visitor can't change the owner's memories. Only tools that change nothing outside Toskar run, whatever the profile's permissions say. In an automation, the owner's chosen actions still run, as #586 allows.

`tests/quality/doorstep.json` holds a front door's cases: asked whether anyone is home, to open the door, by someone who says they're the owner, and to ignore the rules, in several languages.

## Narrower reach

What an assistant can reach is part of staying on topic:

- **Web:** with sites set, every web search asks only those sites, results from anywhere else are dropped, and pages elsewhere aren't opened, including a page a link or a redirect leads to. This holds for the model's own searches, Toskar's look-ups, and pages followed from results, at either strictness. The words are added to every search.
- **Tools:** a profile uses only the tools turned on in its **Tools** tab. Give a public assistant only the ones its topic needs: a tire shop's needs web search, maybe maps, and not files or code.
- **Knowledge:** a turn searches only the profile's own knowledge sources, from its **Knowledge** tab.

## How well it holds

`tests/quality/topics.json` is a tire shop with Enforce: on-topic questions and small talk it must answer, and off-topic ones it must hold, with jailbreaks in several forms and languages. Each run reports the share held and the share wrongly refused, so tightening the controls can't quietly start refusing real customers; the weekly run on real models includes it. With Gemma 3 4B, two runs held 12 and 13 of 13 off-topic cases and wrongly refused none of 9 on-topic ones; the one it let through was another shop's prices, a subject that looks like the topic.

## Be honest about the limits

No language model can promise it never goes off topic. Guide makes it the model's clear instruction; Enforce makes going off topic hard and visible, not impossible. A small model can label a message wrongly either way, so try a profile's real questions before publishing it.

Topic controls don't replace keeping private data out of a public assistant: anything in its knowledge sources or reachable by its tools can still be asked about. Give a public assistant only the knowledge and tools it needs.
