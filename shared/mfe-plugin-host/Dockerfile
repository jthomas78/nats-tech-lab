FROM golang:1.27-alpine AS build
WORKDIR /src
COPY shared/mferegistry ./shared/mferegistry
COPY shared/natsconn ./shared/natsconn
COPY shared/mfe-plugin-host ./shared/mfe-plugin-host
WORKDIR /src/shared/mfe-plugin-host
RUN go mod download
RUN CGO_ENABLED=0 go build -o /out/mfe-plugin-host .

FROM alpine:3.21
RUN adduser -D app \
    && mkdir -p /srv /var/lib/announcer \
    && chown -R app:app /srv /var/lib/announcer
USER app
COPY --from=build /out/mfe-plugin-host /usr/local/bin/mfe-plugin-host
EXPOSE 8080
ENTRYPOINT ["mfe-plugin-host"]
