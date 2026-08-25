FROM golang:1.26-alpine AS builder
WORKDIR /workspace
COPY pkg/ ./pkg/
COPY libs/ ./libs/
COPY services/file-service/go.mod services/file-service/go.sum* ./services/file-service/
WORKDIR /workspace/services/file-service
RUN go mod download
COPY services/file-service/ .
RUN CGO_ENABLED=0 go build -o /bin/server ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /bin/server /server
EXPOSE 8101
ENTRYPOINT ["/server"]
