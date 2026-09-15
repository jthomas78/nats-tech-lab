# Demo context isolation

Treat every `demos/<nn>-*` directory as an isolated task and AI context. Do not
infer a demo's terminology, architecture, subject conventions, resources, or
constraints from another demo. Establish the active demo from the user's stated
scope and inspect that demo's files before applying repository-wide assumptions.

For the NATS CQRS/write-side questions and diagram discussion started on
2026-09-14, the active scope is `demos/03-multi-cluster-and-accounts`, even if an
artifact is temporarily located under another demo directory.

Within that demo's CQRS model, one canonical JetStream stream is both the write
side's input stream and its authoritative event history. Treat "input" and
"authoritative" as roles of the same stream; do not draw or describe them as two
separate JetStream streams.
