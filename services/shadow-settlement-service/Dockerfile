FROM golang:1.26-alpine AS builder
WORKDIR /workspace
COPY pkg/ ./pkg/
COPY libs/ ./libs/
COPY services/shadow-settlement-service/go.mod services/shadow-settlement-service/go.sum* ./services/shadow-settlement-service/
WORKDIR /workspace/services/shadow-settlement-service
RUN go mod download
COPY services/shadow-settlement-service/ .
RUN CGO_ENABLED=0 go build -o /bin/server ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /bin/server /server
EXPOSE 8090
ENTRYPOINT ["/server"]
