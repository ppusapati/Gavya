FROM golang:1.26-alpine AS builder
WORKDIR /workspace
COPY pkg/ ./pkg/
COPY libs/ ./libs/
COPY services/canonical-service/go.mod services/canonical-service/go.sum* ./services/canonical-service/
WORKDIR /workspace/services/canonical-service
RUN go mod download
COPY services/canonical-service/ .
RUN CGO_ENABLED=0 go build -o /bin/server ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /bin/server /server
EXPOSE 8093
ENTRYPOINT ["/server"]
