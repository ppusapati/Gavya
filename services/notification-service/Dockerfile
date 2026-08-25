FROM golang:1.26-alpine AS builder
WORKDIR /workspace
COPY services/notification-service/go.mod services/notification-service/go.sum* ./services/notification-service/
COPY pkg/ ./pkg/
COPY libs/ ./libs/
WORKDIR /workspace/services/notification-service
RUN go mod download
COPY services/notification-service/ .
RUN CGO_ENABLED=0 go build -o /bin/server ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /bin/server /server
EXPOSE 8098
ENTRYPOINT ["/server"]
