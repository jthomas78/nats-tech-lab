FROM golang:1.27-alpine AS build
WORKDIR /src
# Phase 35: build context is the repo root (see deploy/cell/compose.runtime.yaml) so this
# can COPY the shared/* modules alongside the service source — the same pattern
# the frontend Dockerfiles already use for shared/unifi-theme.
COPY shared/natsconn ./shared/natsconn
COPY shared/natstenants ./shared/natstenants
COPY shared/browserrpc ./shared/browserrpc
# Phase 54a-4: the OTel glue. Built in always; used only when
# compose.runtime.yaml sets OTEL_EXPORTER_OTLP_ENDPOINT.
COPY shared/otelnats ./shared/otelnats
COPY demos/01-dictionary/backend/organizations-service ./demos/01-dictionary/backend/organizations-service
WORKDIR /src/demos/01-dictionary/backend/organizations-service
RUN go mod download
RUN CGO_ENABLED=0 go build -o /out/organizations-service ./cmd

FROM alpine:3.21
RUN adduser -D app
USER app
COPY --from=build /out/organizations-service /usr/local/bin/organizations-service
EXPOSE 8080
ENTRYPOINT ["organizations-service"]
