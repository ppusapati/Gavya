FROM golang:1.26-alpine AS builder
WORKDIR /workspace
COPY services/feed-service/go.mod services/feed-service/go.sum* ./services/feed-service/
COPY pkg/ ./pkg/
COPY libs/ ./libs/
WORKDIR /workspace/services/feed-service
RUN go mod download
COPY services/feed-service/ .
RUN CGO_ENABLED=0 go build -o /bin/server ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /bin/server /server
EXPOSE 8085
ENTRYPOINT ["/server"]
