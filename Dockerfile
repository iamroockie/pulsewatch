FROM golang:1.27.1-alpine3.24 AS build
ENV CGO_ENABLED=0
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go install -trimpath -ldflags="-s -w" \
        -tags="no_clickhouse no_libsql no_mssql no_mysql no_sqlite3 no_vertica no_ydb" \
        github.com/pressly/goose/v3/cmd/goose@$(go list -m -f '{{.Version}}' github.com/pressly/goose/v3)
COPY cmd cmd
COPY internal internal
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -ldflags="-s -w" -o /out/ ./cmd/...

FROM alpine:3.24.2 AS runtime
USER nobody

FROM runtime AS api
COPY --from=build /out/api /usr/local/bin/
ENTRYPOINT ["api"]

FROM runtime AS worker
COPY --from=build /out/worker /usr/local/bin/
ENTRYPOINT ["worker"]

FROM runtime AS migrate
ENV GOOSE_DRIVER=postgres GOOSE_MIGRATION_DIR=/migrations
COPY --from=build /go/bin/goose /usr/local/bin/
COPY migrations/sql /migrations
ENTRYPOINT ["goose"]
CMD ["up"]
