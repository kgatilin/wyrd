# syntax=docker/dockerfile:1

# The image of the wyrd daemon for a container platform: the wyrd binary with
# the review UI embedded, plus what it needs at run time to read a Go
# repository — the Go toolchain (the reader shells out to `go list`), git and
# CA certificates. All three come with the golang base image.
#
#   docker build -t wyrd:dev --build-arg VERSION=$(git describe --tags --always) .
#
# GO_VERSION follows go.mod; NODE_VERSION follows the CI workflows.
ARG GO_VERSION=1.25.1
ARG NODE_VERSION=22

FROM node:${NODE_VERSION}-bookworm-slim AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:${GO_VERSION}-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# web/dist is embedded into the binary and is not in git: take the one the
# stage above built, whatever the build context happened to carry.
COPY --from=web /src/web/dist ./web/dist
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.Version=${VERSION}" -o /out/wyrd ./cmd/wyrd

FROM golang:${GO_VERSION}-bookworm
ARG VERSION=dev
LABEL org.opencontainers.image.title="wyrd" \
      org.opencontainers.image.description="Code graph, search and review UI for Go repositories" \
      org.opencontainers.image.source="https://github.com/kgatilin/wyrd" \
      org.opencontainers.image.version="${VERSION}"

# The checkout wyrd serves is written by something else — a sync container,
# an init step — and usually as another user, which git refuses to read
# without this.
RUN git config --system --add safe.directory '*' \
 && useradd --uid 10001 --create-home --home-dir /home/wyrd wyrd \
 && mkdir -p /data && chown wyrd:wyrd /data

COPY --from=build /out/wyrd /usr/local/bin/wyrd

# Everything wyrd and the Go toolchain write goes under /data, the one path a
# deployment has to persist: the model store and daemon registry (WYRD_HOME),
# the module cache the reader's `go list` fills, and the build cache.
ENV WYRD_HOME=/data/wyrd \
    GOMODCACHE=/data/go/mod \
    GOCACHE=/data/go/build

USER 10001:10001
WORKDIR /data
EXPOSE 8080
ENTRYPOINT ["wyrd"]
CMD ["serve", "--repo", "/data/repo", "--http", "0.0.0.0:8080"]
