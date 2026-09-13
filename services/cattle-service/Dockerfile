FROM golang:1.26-alpine AS builder
WORKDIR /workspace
COPY services/cattle-service/go.mod services/cattle-service/go.sum* ./services/cattle-service/
COPY pkg/ ./pkg/
COPY libs/ ./libs/
WORKDIR /workspace/services/cattle-service
RUN go mod download
COPY services/cattle-service/ .
# -trimpath strips the build machine's paths from the binary and -s -w drop the
# symbol and DWARF tables; neither is needed at runtime and together they take a
# third off every image, thirty times over.
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /bin/server ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /bin/server /server
EXPOSE 8081
ENTRYPOINT ["/server"]
