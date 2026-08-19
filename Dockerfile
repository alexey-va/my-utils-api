# syntax=docker/dockerfile:1

FROM golang:1.26.5-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
	/usr/local/go/bin/go mod download

COPY cmd cmd
COPY internal internal
COPY src/main/resources/db/migration src/main/resources/db/migration

ARG GIT_COMMIT=unknown
RUN --mount=type=cache,target=/go/pkg/mod \
	--mount=type=cache,target=/root/.cache/go-build \
	CGO_ENABLED=0 /usr/local/go/bin/go build \
	-trimpath -buildvcs=false -ldflags="-s -w -X main.gitCommit=${GIT_COMMIT}" \
	-o /out/my-utils-api ./cmd/my-utils-api

FROM alpine:3.23
WORKDIR /app

RUN apk add --no-cache ca-certificates \
	&& addgroup -S -g 10001 myutils \
	&& adduser -S -D -H -u 10001 -G myutils myutils

COPY --from=build /out/my-utils-api /app/my-utils-api

USER myutils:myutils
EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=5s --start-period=15s --retries=8 \
	CMD ["/app/my-utils-api", "healthcheck"]

ENTRYPOINT ["/app/my-utils-api"]
