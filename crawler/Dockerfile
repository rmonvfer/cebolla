FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/crawler ./cmd/crawler && mkdir -p /out/report

# distroless: no shell, no package manager; CA certificates for seedsync.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/crawler /crawler
# Seeds the report volume with the right owner on first mount.
COPY --from=build --chown=65532:65532 /out/report /report
USER nonroot:nonroot
ENTRYPOINT ["/crawler"]
CMD ["run"]
