FROM golang:1.26-alpine AS builder
WORKDIR /workspace
COPY services/billing-service/go.mod services/billing-service/go.sum* ./services/billing-service/
COPY pkg/ ./pkg/
COPY libs/ ./libs/
WORKDIR /workspace/services/billing-service
RUN go mod download
COPY services/billing-service/ .
# -trimpath strips the build machine's paths from the binary and -s -w drop the
# symbol and DWARF tables; neither is needed at runtime and together they take a
# third off every image, thirty times over.
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /bin/server ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /bin/server /server
EXPOSE 8097
ENTRYPOINT ["/server"]
