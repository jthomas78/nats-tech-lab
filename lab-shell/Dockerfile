# syntax=docker/dockerfile:1.7-labs
#
# Build context is the REPO ROOT (see demos/01-dictionary/deploy/cell/compose.runtime.yaml)
# so the repo-root shared UniFi theme preset (@unifi-theme), the shared
# AppShell (@ui-shell) and demo-01's shared NATS URL resolver (@nats-shared)
# can be copied in alongside the shell's own source.
#
# What this build deliberately does NOT copy is lab-shell/plugins/. The whole
# claim of BR-AS03 is that the host never compiles a plugin, and an image is
# the easiest place to break that by accident — so the plugin's source never
# enters this build context at all. It has its own Dockerfile, its own image
# and its own port; the shell still learns its URL from the registry at
# runtime, exactly as it does in development.
FROM node:24-alpine AS build
WORKDIR /repo
COPY shared ./shared
COPY demos/01-dictionary/shared ./demos/01-dictionary/shared
COPY lab-shell/package.json lab-shell/package-lock.json ./lab-shell/
COPY lab-shell/index.html lab-shell/vite.config.js ./lab-shell/
COPY lab-shell/src ./lab-shell/src
# vite.config.js has imported these since 16b: they generate the build
# catalogue, the plugin asset layout and the demo-readiness routing rules.
COPY lab-shell/tools ./lab-shell/tools
# The demos' own metadata, and ONLY their metadata. The readiness rules are
# generated from the same repository scan the catalogue uses (BR-AS81), so the
# scan has to be able to run in here — but a `manifest.json` and a `demo.json`
# are all it reads. `--parents` keeps each file at its real path, and the
# wildcard keeps this line derived rather than a hand-written list of demos.
#
# No demo SOURCE and no demo `dist/` enters this context, so the claim at the
# top of this file still holds: the host never compiles a plugin. This image is
# a `plugin-source: registry` shell, which resolves every remote from the
# curated registry and serves no plugin assets of its own.
COPY --parents demos/*/frontend/public/manifest.json demos/*/frontend/public/demo.json ./
WORKDIR /repo/lab-shell
RUN npm install && npm run build

FROM nginx:1.27-alpine
COPY lab-shell/nginx.conf /etc/nginx/conf.d/default.conf
# The generated demo-readiness rules (BR-AS79). The build writes them into
# dist/deploy/; they are moved OUT of the served tree here, because they are a
# deployment's routing config and name upstreams no page needs to know.
COPY --from=build /repo/lab-shell/dist/deploy/demo-readiness.conf /etc/nginx/demo-readiness.conf
# The generated demo API routes (BR-AS82), moved out of the served tree for
# the same reason.
COPY --from=build /repo/lab-shell/dist/deploy/demo-api.conf /etc/nginx/demo-api.conf
# The generated hosted plugin-asset routes (BR-AS77, task 16l). Same move, and
# the same reason: it is routing config naming upstreams no page needs. This
# is what makes `/plugins/<id>/…` answer in an image that, as the note at the
# top of this file says, packages no plugin of its own — the files come from
# the demo's image over the network, not from a COPY here.
COPY --from=build /repo/lab-shell/dist/deploy/demo-assets.conf /etc/nginx/demo-assets.conf
COPY --from=build /repo/lab-shell/dist /usr/share/nginx/html
RUN rm -rf /usr/share/nginx/html/deploy
EXPOSE 80
