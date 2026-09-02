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
COPY lab-shell/plugins/example-plugin-activate-throws/package.json lab-shell/plugins/example-plugin-activate-throws/package-lock.json ./lab-shell/plugins/example-plugin-activate-throws/
COPY lab-shell/plugins/example-plugin-activate-throws/index.html lab-shell/plugins/example-plugin-activate-throws/vite.config.js ./lab-shell/plugins/example-plugin-activate-throws/
COPY lab-shell/plugins/example-plugin-activate-throws/src ./lab-shell/plugins/example-plugin-activate-throws/src
COPY lab-shell/plugins/example-plugin-activate-throws/public ./lab-shell/plugins/example-plugin-activate-throws/public
WORKDIR /repo/lab-shell/plugins/example-plugin-activate-throws
RUN npm ci && npm run build

FROM mfe-plugin-host
COPY --from=build /repo/lab-shell/plugins/example-plugin-activate-throws/dist /srv
