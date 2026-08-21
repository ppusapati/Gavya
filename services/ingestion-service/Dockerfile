FROM golang:1.26-alpine AS builder
WORKDIR /workspace
COPY pkg/ ./pkg/
COPY libs/ ./libs/
COPY services/ingestion-service/go.mod services/ingestion-service/go.sum* ./services/ingestion-service/
WORKDIR /workspace/services/ingestion-service
RUN go mod download
COPY services/ingestion-service/ .
RUN CGO_ENABLED=0 go build -o /bin/server ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /bin/server /server
EXPOSE 8091
ENTRYPOINT ["/server"]
