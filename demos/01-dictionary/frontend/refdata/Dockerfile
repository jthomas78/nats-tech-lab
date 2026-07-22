# Build context is the REPO ROOT (see docker-compose.yml) so the shared
# UniFi theme preset can be copied in alongside the app.
FROM node:24-alpine AS build
WORKDIR /repo
COPY shared ./shared
COPY demos/01-dictionary/frontend/refdata ./demos/01-dictionary/frontend/refdata
WORKDIR /repo/demos/01-dictionary/frontend/refdata
RUN npm install && npm run build

FROM nginx:1.27-alpine
COPY demos/01-dictionary/frontend/refdata/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /repo/demos/01-dictionary/frontend/refdata/dist /usr/share/nginx/html
EXPOSE 80
