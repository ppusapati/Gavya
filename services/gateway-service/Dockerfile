FROM golang:1.26-alpine AS builder
WORKDIR /workspace
COPY pkg/ ./pkg/
COPY libs/ ./libs/
COPY services/gateway-service/go.mod services/gateway-service/go.sum* ./services/gateway-service/
WORKDIR /workspace/services/gateway-service
RUN go mod download
COPY services/gateway-service/ .
RUN CGO_ENABLED=0 go build -o /bin/server ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /bin/server /server
EXPOSE 8000
ENTRYPOINT ["/server"]
