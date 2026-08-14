FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/trading-partner-service ./cmd

FROM alpine:3.21
RUN adduser -D app
USER app
COPY --from=build /out/trading-partner-service /usr/local/bin/trading-partner-service
EXPOSE 8080
ENTRYPOINT ["trading-partner-service"]
