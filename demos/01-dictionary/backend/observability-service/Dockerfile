FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go install github.com/swaggo/swag/cmd/swag@v1.16.6
RUN swag init --generalInfo cmd/main.go --dir . --output docs/ --parseDependency=false --parseInternal
RUN CGO_ENABLED=0 go build -o /out/observability-service ./cmd

FROM alpine:3.21
RUN adduser -D app
USER app
COPY --from=build /out/observability-service /usr/local/bin/observability-service
EXPOSE 8080
ENTRYPOINT ["observability-service"]
