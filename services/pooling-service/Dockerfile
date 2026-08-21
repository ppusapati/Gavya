FROM golang:1.26-alpine AS builder
WORKDIR /workspace
COPY pkg/ ./pkg/
COPY libs/ ./libs/
COPY services/pooling-service/go.mod services/pooling-service/go.sum* ./services/pooling-service/
WORKDIR /workspace/services/pooling-service
RUN go mod download
COPY services/pooling-service/ .
RUN CGO_ENABLED=0 go build -o /bin/server ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /bin/server /server
EXPOSE 8094
ENTRYPOINT ["/server"]
