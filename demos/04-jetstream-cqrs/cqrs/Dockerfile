# Demo 04's command API, as its own image (app-shell task 16l).
#
# Built for the PACKAGED arrangement only. `deploy/start.sh` is still the way
# this demo is run on a laptop: it builds this same binary on the host and
# runs `serve`, `snapshotter` and `projector` against the published NATS port.
# This image exists so that `deploy/compose.shell.yaml` can put `serve` on a
# network the lab shell's container can reach, which a host process cannot be.
#
# Context is this folder. The module has its own go.mod and go.sum, so the
# repo-root go.work is neither needed nor wanted here — GOFLAGS turns it off
# rather than leaving the build to guess.
FROM golang:1.27-alpine AS build
ENV GOWORK=off
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /out/cqrs .

FROM alpine:3.22
COPY --from=build /out/cqrs /usr/local/bin/cqrs
# No default command: main.go dispatches on the first argument, and
# compose.shell.yaml says which one this container is.
ENTRYPOINT ["cqrs"]
