FROM golang:1.26-alpine AS build
WORKDIR /src
# Phase 35: build context is the repo root (see docker-compose.yml) so this
# can COPY shared/natstrace alongside the service source — the same pattern
# the frontend Dockerfiles already use for shared/unifi-theme.
COPY shared/natstrace ./shared/natstrace
COPY shared/natsconn ./shared/natsconn
COPY shared/jstream ./shared/jstream
COPY shared/natsnotify ./shared/natsnotify
COPY shared/natstest ./shared/natstest
COPY shared/natstenants ./shared/natstenants
COPY shared/browserrpc ./shared/browserrpc
COPY demos/01-dictionary/backend/shipping-service ./demos/01-dictionary/backend/shipping-service
WORKDIR /src/demos/01-dictionary/backend/shipping-service
RUN go mod download
RUN go install github.com/swaggo/swag/cmd/swag@latest
RUN swag init --generalInfo cmd/main.go --dir . --output docs/ --parseDependency=false --parseInternal
RUN CGO_ENABLED=0 go build -o /out/dictionary ./cmd

FROM alpine:3.21
RUN adduser -D app
USER app
COPY --from=build /out/dictionary /usr/local/bin/dictionary
EXPOSE 8080
ENTRYPOINT ["dictionary"]
