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

FROM nginx:1.27-alpine
COPY lab-shell/plugins/demo-catalog/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /repo/lab-shell/plugins/demo-catalog/dist /usr/share/nginx/html
EXPOSE 80
