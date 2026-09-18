# The whole platform as one image.
#
# This is the shape that ships, and until the images were built anywhere it was
# the one Dockerfile in the repository that could not build at all: it copied
# go.work, which lists ./e2e, ./tools/amcu and ./tools/dbadmin, and copied none
# of them, so the first `go build` stopped at "cannot load module ../../e2e
# listed in go.work file". Thirty Dockerfiles, and the only one nobody could
# have built was the one a deployment uses. `go build` in the gate had stood in
# for building the image for as long as there had been an image.
#
# Every module is copied because the modulith imports all of them, and the
# workspace is copied for the reason the other twenty-nine now copy it too:
# go.work decides which version of pgx, golang.org/x/crypto and connect the
# binary links against. That reasoning sits beside the test that enforces it,
# TestEveryImageIsBuiltFromTheWorkspaceThatWasTested.
FROM golang:1.26-alpine AS builder
WORKDIR /workspace

# e2e and the two tools arrive as bare go.mod files. They are listed in go.work,
# so the build refuses to start without them, and they are the test suite and the
# operator tools, which have no business in a release image. The module graph
# needs the go.mod and nothing else.
COPY go.work go.work.sum ./
COPY pkg/ ./pkg/
COPY libs/ ./libs/
COPY e2e/go.mod ./e2e/
COPY tools/amcu/go.mod ./tools/amcu/
COPY tools/dbadmin/go.mod ./tools/dbadmin/
COPY services/ ./services/

# Identical in every one of these Dockerfiles, on purpose: the layer is shared,
# so building all thirty images in one place downloads the dependencies once
# rather than thirty times.
RUN go mod download all

WORKDIR /workspace/services/modulith
# -trimpath strips the build machine's paths from the binary and -s -w drop the
# symbol and DWARF tables; neither is needed at runtime and together they take a
# third off every image, thirty times over.
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /bin/gavya ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /bin/gavya /gavya
# One port, which is the point of this image: see the note at the top of
# cmd/server/main.go.
#
# Eight thousand and not 8080, which is what this said before. The modulith is
# the gateway with everything else behind it in one process, so it reads the
# gateway's configuration and listens where ports.Gateway says when nothing
# overrides it. docker-compose.modulith.yaml sets SERVER_ADDR=:8080 and is free
# to; this line is about the image run with nothing set, and `docker run -P` on
# it published 8080 while the process was listening on 8000.
EXPOSE 8000
ENTRYPOINT ["/gavya"]
