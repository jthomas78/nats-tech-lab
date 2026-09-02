# Build context is the REPO ROOT (see demos/01-dictionary/docker-compose.yml)
# so the shared UniFi theme preset (@unifi-theme) can be copied in.
#
# This image is the containerised form of BR-AS15's claim: the plugin is built
# by its own toolchain, from its own package.json and its own lockfile, into
# its own image, and served from its own port. Nothing here imports the shell
# and nothing here is compiled by it — the two Dockerfiles share only the
# theme directory, which is a stylesheet, not an API.
FROM node:24-alpine AS build
WORKDIR /repo
COPY shared ./shared
# The README is plugin content, never compiled into the host.
COPY demos/01-dictionary/README.md ./demos/01-dictionary/README.md
COPY lab-shell/plugins/demo-catalog/package.json lab-shell/plugins/demo-catalog/package-lock.json ./lab-shell/plugins/demo-catalog/
COPY lab-shell/plugins/demo-catalog/index.html lab-shell/plugins/demo-catalog/vite.config.js ./lab-shell/plugins/demo-catalog/
COPY lab-shell/plugins/demo-catalog/src ./lab-shell/plugins/demo-catalog/src
COPY lab-shell/plugins/demo-catalog/public ./lab-shell/plugins/demo-catalog/public
WORKDIR /repo/lab-shell/plugins/demo-catalog
RUN npm ci && npm run build

# The shared Go host, like every other plugin (Phase 15c). The catalogue
# plugin used to be the one that ran nginx, because it is preloaded rather
# than announced and so had nothing to announce with. Once health became
# something a plugin PUBLISHES rather than something the registry dials
# (decision 14), that exception stopped being free: nginx cannot hold a NATS
# credential, so the one plugin on a different server would have been the one
# plugin with no health at all.
#
# It runs the host in HEALTH_ONLY mode: it serves its assets and reports
# itself, and it never announces. Its behaviour is otherwise the same as the
# nginx it replaces, including the absence of an SPA fallback — the host 404s
# a missing file rather than answering index.html, which is what keeps the
# `demo-catalog-unreachable` fixture failing at the fetch and not later as a
# module parse error.
FROM mfe-plugin-host
COPY --from=build /repo/lab-shell/plugins/demo-catalog/dist /srv
