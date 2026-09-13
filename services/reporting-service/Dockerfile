FROM golang:1.26-alpine AS builder
WORKDIR /workspace
COPY pkg/ ./pkg/
COPY libs/ ./libs/
COPY services/reporting-service/go.mod services/reporting-service/go.sum* ./services/reporting-service/
WORKDIR /workspace/services/reporting-service
RUN go mod download
COPY services/reporting-service/ .
# -trimpath strips the build machine's paths from the binary and -s -w drop the
# symbol and DWARF tables; neither is needed at runtime and together they take a
# third off every image, thirty times over.
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /bin/server ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /bin/server /server
EXPOSE 8099
ENTRYPOINT ["/server"]
