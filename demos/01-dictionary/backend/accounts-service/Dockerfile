FROM golang:1.27-alpine AS build
WORKDIR /src
# Phase 35: build context is the repo root (see deploy/global/compose.control.yaml) so this
# can COPY shared/natstrace alongside the service source — the same pattern
# the frontend Dockerfiles already use for shared/unifi-theme.
COPY shared/natstrace ./shared/natstrace
COPY shared/natsconn ./shared/natsconn
COPY shared/natsnotify ./shared/natsnotify
# Phase 50b: accounts/usersrpc.go answers the browser over api.* through the
# shared browserrpc envelope, so its module has to be in the build context too.
COPY shared/browserrpc ./shared/browserrpc
# The registry left for mfe-registry-service, but auth still names its subjects
# when it mints the shell's and the operator's browser credentials — from this
# shared contract module rather than local literals (BR-AS25/AS27).
COPY shared/mferegistry ./shared/mferegistry
COPY demos/01-dictionary/backend/accounts-service ./demos/01-dictionary/backend/accounts-service
WORKDIR /src/demos/01-dictionary/backend/accounts-service
RUN go mod download
RUN go install github.com/swaggo/swag/cmd/swag@v1.16.6
RUN swag init --generalInfo cmd/main.go --dir . --output docs/ --parseDependency=false --parseInternal
RUN CGO_ENABLED=0 go build -o /out/accounts-service ./cmd

FROM alpine:3.21
RUN adduser -D app
# BR-AC46 -- the writable creds directory, pre-created and owned by `app`
# BEFORE the USER switch. A named volume mounted at a path that exists in
# the image is seeded from that path on first use, ownership included; a
# volume mounted at a path the image does NOT have is created root:root 0755
# and this non-root process cannot write to it (tested: "open
# /var/lib/nats/creds/<tenant>.creds: permission denied" on mint). Compose
# has no way to set a volume's owner, so the image has to carry it.
RUN mkdir -p /var/lib/nats/creds && chown app:app /var/lib/nats/creds
USER app
COPY --from=build /out/accounts-service /usr/local/bin/accounts-service
EXPOSE 8080
ENTRYPOINT ["accounts-service"]
