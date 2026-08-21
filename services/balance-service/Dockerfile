FROM golang:1.26-alpine AS builder
WORKDIR /workspace
COPY pkg/ ./pkg/
COPY libs/ ./libs/
COPY services/balance-service/go.mod services/balance-service/go.sum* ./services/balance-service/
WORKDIR /workspace/services/balance-service
RUN go mod download
COPY services/balance-service/ .
RUN CGO_ENABLED=0 go build -o /bin/server ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /bin/server /server
EXPOSE 8095
ENTRYPOINT ["/server"]
