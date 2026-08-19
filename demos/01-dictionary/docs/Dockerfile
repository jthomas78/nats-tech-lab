# Self-contained build context (unlike the frontend/ apps, this site imports
# nothing from shared/unifi-theme — its palette is reproduced locally in
# .vitepress/theme/custom.css, see CLAUDE.md's Frontend Design System
# "Exception" note), so the context is this directory, not the repo root.
FROM node:24-alpine AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM nginx:1.27-alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/.vitepress/dist /usr/share/nginx/html
EXPOSE 80
