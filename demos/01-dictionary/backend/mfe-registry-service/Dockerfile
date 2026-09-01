FROM golang:1.27-alpine AS build
WORKDIR /src
# Build context is the repo root (see docker-compose.yml) so this can COPY the
# shared modules alongside the service source — the same pattern every other
# service block already uses.
COPY shared/browserrpc ./shared/browserrpc
COPY shared/mferegistry ./shared/mferegistry
COPY shared/natsconn ./shared/natsconn
COPY shared/natsnotify ./shared/natsnotify
COPY shared/natstrace ./shared/natstrace
COPY demos/01-dictionary/backend/mfe-registry-service ./demos/01-dictionary/backend/mfe-registry-service
WORKDIR /src/demos/01-dictionary/backend/mfe-registry-service
RUN go mod download
# No swag step: the registry has no HTTP API surface to document — it answers
# on api.* subjects only, and its rest package is an exhaustively empty mount.
RUN CGO_ENABLED=0 go build -o /out/mfe-registry-service ./cmd
# Second binary in the same image, not a second image: seed-publishers is the
# boot-time trust seeder (app-shell 13d), and Compose runs it as a one-shot by
# overriding the entrypoint below. Sharing the image keeps the two in lockstep
# — a seeder built from older source than the registry it writes to is exactly
# the drift the convergence rule cannot see.
RUN CGO_ENABLED=0 go build -o /out/seed-publishers ./cmd/seed-publishers
# Third binary, same reasoning: announce-plugin is the resident publisher
# sidecar (app-shell 13e). One image per plugin would mean five images whose
# only difference is a mounted manifest.
RUN CGO_ENABLED=0 go build -o /out/announce-plugin ./cmd/announce-plugin

FROM alpine:3.21
RUN adduser -D app
# Created before USER so a fresh named volume mounted here inherits app's
# ownership. announce-plugin runs unprivileged and must write its release
# counter (BR-AS67); a root-owned volume would fail it on the first announce.
RUN mkdir -p /var/lib/announcer && chown app:app /var/lib/announcer
USER app
COPY --from=build /out/mfe-registry-service /usr/local/bin/mfe-registry-service
COPY --from=build /out/seed-publishers /usr/local/bin/seed-publishers
COPY --from=build /out/announce-plugin /usr/local/bin/announce-plugin
EXPOSE 8080
ENTRYPOINT ["mfe-registry-service"]
