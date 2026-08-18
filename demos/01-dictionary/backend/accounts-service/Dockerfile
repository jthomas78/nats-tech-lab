FROM golang:1.26-alpine AS build
WORKDIR /src
# Phase 35: build context is the repo root (see docker-compose.yml) so this
# can COPY shared/natstrace alongside the service source — the same pattern
# the frontend Dockerfiles already use for shared/unifi-theme.
COPY shared/natstrace ./shared/natstrace
COPY demos/01-dictionary/backend/accounts-service ./demos/01-dictionary/backend/accounts-service
WORKDIR /src/demos/01-dictionary/backend/accounts-service
RUN go mod download
RUN CGO_ENABLED=0 go build -o /out/accounts-service ./cmd

FROM alpine:3.21
RUN adduser -D app
USER app
COPY --from=build /out/accounts-service /usr/local/bin/accounts-service
EXPOSE 8080
ENTRYPOINT ["accounts-service"]
