FROM golang:1.26-alpine AS builder
WORKDIR /workspace
COPY services/product-catalog-service/go.mod services/product-catalog-service/go.sum* ./services/product-catalog-service/
COPY pkg/ ./pkg/
COPY libs/ ./libs/
WORKDIR /workspace/services/product-catalog-service
RUN go mod download
COPY services/product-catalog-service/ .
RUN CGO_ENABLED=0 go build -o /bin/server ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /bin/server /server
EXPOSE 8088
ENTRYPOINT ["/server"]
