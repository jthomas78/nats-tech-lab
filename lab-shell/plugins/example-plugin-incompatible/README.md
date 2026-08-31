# Example Plugin (future shell API)

Independent fixture on port 7115. This remote requires shell API 2 and must be rejected before loading.

Run `npm ci && npm run dev` here, or `docker compose up --build -d example-plugin-incompatible-frontend`
from `demos/01-dictionary/`. Its single federation exposure is `plugin`; its
self-description is served at `/manifest.json`. Operator curation lives only in
`demos/01-dictionary/registry.json`.
