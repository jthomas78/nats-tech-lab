FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/otlp-bridge ./cmd

FROM alpine:3.21
RUN adduser -D app
USER app
COPY --from=build /out/otlp-bridge /usr/local/bin/otlp-bridge
ENTRYPOINT ["otlp-bridge"]
