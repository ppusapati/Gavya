FROM golang:1.26-alpine AS builder
WORKDIR /workspace
COPY pkg/ ./pkg/
COPY libs/ ./libs/
COPY services/identity-service/go.mod services/identity-service/go.sum* ./services/identity-service/
WORKDIR /workspace/services/identity-service
RUN go mod download
COPY services/identity-service/ .
RUN CGO_ENABLED=0 go build -o /bin/server ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /bin/server /server
EXPOSE 8102
ENTRYPOINT ["/server"]
