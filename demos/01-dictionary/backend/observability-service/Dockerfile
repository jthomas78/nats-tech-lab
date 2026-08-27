FROM golang:1.27-alpine AS build
WORKDIR /src
# Phase 43d/43e: context is the repo root (see docker-compose.yml) so the build
# can COPY the shared modules this service depends on alongside its own source
# — the same pattern the other backend services already use. Before 43d this
# service depended on nothing under shared/ and built from its own directory;
# adopting shared/natsnotify made that impossible, since a Dockerfile cannot
# reach outside its build context.
COPY shared/natsnotify ./shared/natsnotify
COPY shared/natsconn ./shared/natsconn
COPY shared/natstrace ./shared/natstrace
COPY shared/natstest ./shared/natstest
COPY demos/01-dictionary/backend/observability-service ./demos/01-dictionary/backend/observability-service
WORKDIR /src/demos/01-dictionary/backend/observability-service
RUN go mod download
RUN go install github.com/swaggo/swag/cmd/swag@v1.16.6
RUN swag init --generalInfo cmd/main.go --dir . --output docs/ --parseDependency=false --parseInternal
RUN CGO_ENABLED=0 go build -o /out/observability-service ./cmd

FROM alpine:3.21
RUN adduser -D app
USER app
COPY --from=build /out/observability-service /usr/local/bin/observability-service
EXPOSE 8080
ENTRYPOINT ["observability-service"]
