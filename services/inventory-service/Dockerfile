FROM golang:1.26-alpine AS builder
WORKDIR /workspace
COPY services/inventory-service/go.mod services/inventory-service/go.sum* ./services/inventory-service/
COPY pkg/ ./pkg/
COPY libs/ ./libs/
WORKDIR /workspace/services/inventory-service
RUN go mod download
COPY services/inventory-service/ .
RUN CGO_ENABLED=0 go build -o /bin/server ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /bin/server /server
EXPOSE 8089
ENTRYPOINT ["/server"]
