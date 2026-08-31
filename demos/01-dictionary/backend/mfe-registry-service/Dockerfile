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

FROM alpine:3.21
RUN adduser -D app
USER app
COPY --from=build /out/mfe-registry-service /usr/local/bin/mfe-registry-service
EXPOSE 8080
ENTRYPOINT ["mfe-registry-service"]
