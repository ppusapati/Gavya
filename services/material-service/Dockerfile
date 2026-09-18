# material-service as one image.
#
# The whole workspace is copied, not just this module. go.work is what decides
# which version of pgx, golang.org/x/crypto and connect this binary links
# against, and the module resolved on its own settles on lower ones than the
# gate ever ran a test against. The reasoning is beside the test that enforces
# it: TestEveryImageIsBuiltFromTheWorkspaceThatWasTested.
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

WORKDIR /workspace/services/material-service
# -trimpath strips the build machine's paths from the binary and -s -w drop the
# symbol and DWARF tables; neither is needed at runtime and together they take a
# third off every image, thirty times over.
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /bin/server ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /bin/server /server
# The port this image listens on when nothing overrides it: libs/integrity/ports.
EXPOSE 8105
ENTRYPOINT ["/server"]
