FROM golang:1.26-alpine AS builder
WORKDIR /workspace
COPY services/health-service/go.mod services/health-service/go.sum* ./services/health-service/
COPY pkg/ ./pkg/
WORKDIR /workspace/services/health-service
RUN go mod download
COPY services/health-service/ .
RUN CGO_ENABLED=0 go build -o /bin/server ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /bin/server /server
EXPOSE 8080
ENTRYPOINT ["/server"]
