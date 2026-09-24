# Demo 04's frontend, as its own image (app-shell task 16l).
#
# Why a separate image at all. The lab shell's packaged image compiles no
# plugin and copies no plugin — that is BR-AS03, and lab-shell/Dockerfile says
# so at the top — so a hosted shell cannot serve `/plugins/demo-04/…` from its
# own tree. It forwards the prefix to this container instead. A plugin stays a
# separate build with a separate lifecycle, which is the point.
#
# Build context is the REPO ROOT, so the shared UniFi theme (@unifi-theme) and
# the shared AppShell (@ui-shell) can be copied in beside this app — the same
# two aliases vite.config.js names, and the same arrangement demo 01's
# frontends use.
FROM node:24-alpine AS build
WORKDIR /repo
COPY shared ./shared
# The About page reads the demo's own prose and drawings with `?raw` (see
# frontend/src/about/sources.js), so these are BUILD inputs, not runtime
# assets. Without them the bundler stops with an unresolved import.
COPY demos/04-jetstream-cqrs/README.md ./demos/04-jetstream-cqrs/README.md
COPY demos/04-jetstream-cqrs/docs ./demos/04-jetstream-cqrs/docs
COPY demos/04-jetstream-cqrs/diagrams ./demos/04-jetstream-cqrs/diagrams
COPY demos/04-jetstream-cqrs/frontend ./demos/04-jetstream-cqrs/frontend
WORKDIR /repo/demos/04-jetstream-cqrs/frontend
RUN npm install && npm run build

FROM nginx:1.27-alpine
COPY demos/04-jetstream-cqrs/frontend/nginx.conf /etc/nginx/conf.d/default.conf
# Placed UNDER the public prefix, not at the document root. The build sets
# `base: '/plugins/demo-04/'`, so every URL this app emits already carries the
# prefix; putting the files anywhere else would need a rewrite, and a rewrite
# is the one thing that puts a proxy and a compiler's own chunk URLs out of
# step.
COPY --from=build /repo/demos/04-jetstream-cqrs/frontend/dist /usr/share/nginx/html/plugins/demo-04
EXPOSE 80
