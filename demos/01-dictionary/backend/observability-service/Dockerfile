FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/observability-service ./cmd

FROM alpine:3.21
RUN adduser -D app
USER app
COPY --from=build /out/observability-service /usr/local/bin/observability-service
ENTRYPOINT ["observability-service"]
