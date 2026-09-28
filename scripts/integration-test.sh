#!/bin/sh
# End-to-end test: real Postgres + OpenSearch, fake tor and fake onion sites.
# Everything runs on an internal Docker network with no outside access, so it
# is safe to run anywhere (it never touches Tor).
set -eu
cd "$(dirname "$0")/.."
NET=onion-it
PW='It-Test-Pw-0nion!'
cleanup() { docker rm -f onion-it-pg onion-it-os >/dev/null 2>&1 || true; docker network rm $NET >/dev/null 2>&1 || true; }
trap cleanup EXIT
cleanup
docker network create --internal $NET >/dev/null
docker run -d --name onion-it-pg --network $NET -e POSTGRES_PASSWORD=pg postgres:18-alpine >/dev/null
docker run -d --name onion-it-os --network $NET \
  -e discovery.type=single-node -e node.store.allow_mmap=false \
  -e OPENSEARCH_JAVA_OPTS='-Xms1g -Xmx1g' -e OPENSEARCH_INITIAL_ADMIN_PASSWORD="$PW" \
  opensearchproject/opensearch:3.8.0 >/dev/null
docker run --rm --network $NET -u "$(id -u):$(id -g)" \
  -e GOCACHE=/cache/build -e GOMODCACHE=/cache/mod -e GOFLAGS=-buildvcs=false -e GOPROXY=off \
  -e DATABASE_URL=postgres://postgres:pg@onion-it-pg:5432/postgres?sslmode=disable \
  -e OPENSEARCH_URL=https://onion-it-os:9200 -e OPENSEARCH_PASSWORD="$PW" \
  -v onion-gocache:/cache -v "$PWD/crawler":/src -w /src golang:1.26-alpine \
  go test -tags integration -count=1 -v ./internal/crawl/ "$@"
