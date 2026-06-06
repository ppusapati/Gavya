FROM golang:1.26-alpine AS builder
WORKDIR /workspace
COPY services/order-service/go.mod services/order-service/go.sum* ./services/order-service/
COPY pkg/ ./pkg/
WORKDIR /workspace/services/order-service
RUN go mod download
COPY services/order-service/ .
RUN CGO_ENABLED=0 go build -o /bin/server ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /bin/server /server
EXPOSE 8080
ENTRYPOINT ["/server"]
