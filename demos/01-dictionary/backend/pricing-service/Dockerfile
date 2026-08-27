FROM golang:1.27-alpine AS build
WORKDIR /src
# Phase 35: build context is the repo root (see docker-compose.yml) so this
# can COPY shared/natstrace alongside the service source — the same pattern
# the frontend Dockerfiles already use for shared/unifi-theme.
COPY shared/natstrace ./shared/natstrace
COPY shared/natsconn ./shared/natsconn
COPY shared/natstenants ./shared/natstenants
COPY shared/browserrpc ./shared/browserrpc
COPY demos/01-dictionary/backend/pricing-service ./demos/01-dictionary/backend/pricing-service
WORKDIR /src/demos/01-dictionary/backend/pricing-service
RUN go mod download
RUN CGO_ENABLED=0 go build -o /out/pricing-service ./cmd

FROM alpine:3.21
RUN adduser -D app
USER app
COPY --from=build /out/pricing-service /usr/local/bin/pricing-service
EXPOSE 8080
ENTRYPOINT ["pricing-service"]
