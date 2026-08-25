FROM golang:1.26-alpine AS builder
WORKDIR /workspace
COPY pkg/ ./pkg/
COPY libs/ ./libs/
COPY services/audit-service/go.mod services/audit-service/go.sum* ./services/audit-service/
WORKDIR /workspace/services/audit-service
RUN go mod download
COPY services/audit-service/ .
RUN CGO_ENABLED=0 go build -o /bin/server ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /bin/server /server
EXPOSE 8100
ENTRYPOINT ["/server"]
