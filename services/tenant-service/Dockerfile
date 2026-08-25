FROM golang:1.26-alpine AS builder
WORKDIR /workspace
COPY services/tenant-service/go.mod services/tenant-service/go.sum* ./services/tenant-service/
COPY pkg/ ./pkg/
COPY libs/ ./libs/
WORKDIR /workspace/services/tenant-service
RUN go mod download
COPY services/tenant-service/ .
RUN CGO_ENABLED=0 go build -o /bin/server ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /bin/server /server
EXPOSE 8080
ENTRYPOINT ["/server"]
