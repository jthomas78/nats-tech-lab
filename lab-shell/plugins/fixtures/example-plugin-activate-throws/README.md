# Example Plugin (activate throws)

Independent fixture on port 7114. This independent remote deliberately fails during activation.

Run `npm ci && npm run dev` here, or `docker compose up --build -d example-plugin-activate-throws-frontend`
from `demos/01-dictionary/`. Its single federation exposure is `plugin`; its
self-description is served at `/manifest.json`. Operator curation lives only in
`demos/01-dictionary/registry.json`.
