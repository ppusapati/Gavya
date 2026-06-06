FROM golang:1.26-alpine AS builder
WORKDIR /workspace
COPY services/farm-service/go.mod services/farm-service/go.sum* ./services/farm-service/
COPY pkg/ ./pkg/
WORKDIR /workspace/services/farm-service
RUN go mod download
COPY services/farm-service/ .
RUN CGO_ENABLED=0 go build -o /bin/server ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /bin/server /server
EXPOSE 8080
ENTRYPOINT ["/server"]
