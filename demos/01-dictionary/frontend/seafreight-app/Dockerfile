# Build context is the REPO ROOT (see docker-compose.yml) so both the
# repo-root shared UniFi theme preset (@unifi-theme) and the demo-local
# shared refdata composables (@refdata) can be copied in alongside the app.
FROM node:24-alpine AS build
WORKDIR /repo
COPY shared ./shared
COPY demos/01-dictionary/shared ./demos/01-dictionary/shared
COPY demos/01-dictionary/backend/refdata-service/refdata/seed.go ./demos/01-dictionary/backend/refdata-service/refdata/seed.go
COPY demos/01-dictionary/frontend/seafreight-app ./demos/01-dictionary/frontend/seafreight-app
WORKDIR /repo/demos/01-dictionary/frontend/seafreight-app
RUN npm install && npm run build

FROM nginx:1.27-alpine
COPY demos/01-dictionary/frontend/seafreight-app/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /repo/demos/01-dictionary/frontend/seafreight-app/dist /usr/share/nginx/html
EXPOSE 80
