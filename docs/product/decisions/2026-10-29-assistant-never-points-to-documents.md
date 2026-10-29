# E10: the assistant never points to a document, it answers

- **Date:** 2026-10-29
- **Decision:** the assistant reads the passages and answers in its own words; no reply, canned or written by the model, tells the person to see, read or open a document, guide or section.
  The fallback names no document; an answer that points to one is rejected by the guard (`points_to_document`).
  An answer rejected for an ungrounded number, an unknown citation or a document pointer is asked once more with what failed, before the fallback; advice and forecasts are never retried.
  The source chips and the AI notice's "check the cited source" stay (L12, PRD §11).
- **Options:** keep the fallback "see the guide, OptionStrategy.md" and show it on the first rejection · answer from the sources, retry once, a fallback that names no document
- **Reason:** "The chatbot should never point out to a document. Instead, it should read it, grab the answers and reply back to the user."; a correct Why? answer was replaced by the fallback for repeating the question's own instrument and time
- **Decided by:** product owner on #534
- **Epic:** E10
