FROM golang:1.26-alpine AS build
WORKDIR /src
# Context is the repo root (see docker-compose.yml), the same pattern the other
# backend services use, so the build can COPY the shared modules this service
# depends on — a Dockerfile cannot reach outside its build context. This
# service built from its own directory until it adopted shared/natsconn.
COPY shared/natsconn ./shared/natsconn
COPY demos/01-dictionary/backend/otlp-bridge ./demos/01-dictionary/backend/otlp-bridge
WORKDIR /src/demos/01-dictionary/backend/otlp-bridge
RUN go mod download
RUN CGO_ENABLED=0 go build -o /out/otlp-bridge ./cmd

FROM alpine:3.21
RUN adduser -D app
USER app
COPY --from=build /out/otlp-bridge /usr/local/bin/otlp-bridge
ENTRYPOINT ["otlp-bridge"]
