FROM golang:1.26-alpine AS builder
WORKDIR /workspace
COPY pkg/ ./pkg/
COPY libs/ ./libs/
COPY services/production-service/go.mod services/production-service/go.sum* ./services/production-service/
WORKDIR /workspace/services/production-service
RUN go mod download
COPY services/production-service/ .
RUN CGO_ENABLED=0 go build -o /bin/server ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /bin/server /server
EXPOSE 8107
ENTRYPOINT ["/server"]
