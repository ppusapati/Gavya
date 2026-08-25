FROM golang:1.26-alpine AS builder
WORKDIR /workspace
COPY services/milk-service/go.mod services/milk-service/go.sum* ./services/milk-service/
COPY pkg/ ./pkg/
COPY libs/ ./libs/
WORKDIR /workspace/services/milk-service
RUN go mod download
COPY services/milk-service/ .
RUN CGO_ENABLED=0 go build -o /bin/server ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /bin/server /server
EXPOSE 8082
ENTRYPOINT ["/server"]
