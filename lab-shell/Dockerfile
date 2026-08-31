# Build context is the REPO ROOT (see demos/01-dictionary/docker-compose.yml)
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
WORKDIR /repo/lab-shell
RUN npm install && npm run build

FROM nginx:1.27-alpine
COPY lab-shell/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /repo/lab-shell/dist /usr/share/nginx/html
EXPOSE 80
