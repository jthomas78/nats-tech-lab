# Example Plugin (slow remote)

Independent fixture on port 7113. This independent remote loads after a six-second delay.

Run `npm ci && npm run dev` here, or `docker compose up --build -d example-plugin-slow-frontend`
from `demos/01-dictionary/`. Its single federation exposure is `plugin`; its
self-description is served at `/manifest.json`. Operator curation lives only in
`demos/01-dictionary/registry.json`.
