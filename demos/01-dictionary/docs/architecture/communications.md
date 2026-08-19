# Communications

<EyebrowLabel text="Architecture" />

Every service exposes both a REST/Swagger surface and a NATS `rpc.*`
surface for the same operations — a dual-transport design. Subjects fall
into four core families (`evt.*`, `rpc.*`, `api.*`, `notify.*`) plus a
supportive `obs.*` debugging side-channel, each with fixed arity so
`{context}` — the company/business-unit scope, never the tenant or
region — can be read by position.

<VerdictBadge status="completed" /> Subject taxonomy running in code
today (Phase 16a onward).

*Content in progress — full subject family reference to follow.*
