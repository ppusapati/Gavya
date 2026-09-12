# The whole platform as one image.
#
# Every module is copied because the modulith imports all of them: their go.mod
# files first, so `go mod download` is cached against dependency changes rather
# than against every edit, then the sources.
FROM golang:1.26-alpine AS builder
WORKDIR /workspace

COPY go.work go.work.sum* ./
COPY pkg/ ./pkg/
COPY libs/ ./libs/
COPY services/ ./services/

WORKDIR /workspace/services/modulith
RUN CGO_ENABLED=0 go build -o /bin/gavya ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /bin/gavya /gavya
# One port. That is the point of this image: see the note at the top of
# cmd/server/main.go.
EXPOSE 8080
ENTRYPOINT ["/gavya"]
