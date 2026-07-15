# Build context is the REPO ROOT (see docker-compose.yml) so both the
# repo-root shared UniFi theme preset (@unifi-theme) and the demo-local
# shared refdata composables (@refdata) can be copied in alongside the app.
FROM node:24-alpine AS build
WORKDIR /repo
COPY shared ./shared
COPY demos/01-dictionary/shared ./demos/01-dictionary/shared
COPY demos/01-dictionary/frontend-port ./demos/01-dictionary/frontend-port
WORKDIR /repo/demos/01-dictionary/frontend-port
RUN npm install && npm run build

FROM nginx:1.27-alpine
COPY demos/01-dictionary/frontend-port/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /repo/demos/01-dictionary/frontend-port/dist /usr/share/nginx/html
EXPOSE 80
