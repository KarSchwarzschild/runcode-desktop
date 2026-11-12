# Knowledge libraries (path A)

Ordinary engine sessions can select up to five knowledge libraries. Selection is persisted per session (account / bridge / tenant scoped), restored on reopen, and frozen for `knowledge_search` at turn start.

There is no independent `kc-*` knowledge-chat session, no `knowledge:chat` event stream, and no Create / Rename / Export knowledge-chat commands. Retrieval runs through the read-only `knowledge_search` tool on the normal ReAct loop.

Detailed stage write-ups and design drafts live outside this repository (see `Knowledge-cut`).
