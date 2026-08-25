FROM golang:1.26-alpine AS builder
WORKDIR /workspace
COPY services/cattle-market-service/go.mod services/cattle-market-service/go.sum* ./services/cattle-market-service/
COPY pkg/ ./pkg/
COPY libs/ ./libs/
WORKDIR /workspace/services/cattle-market-service
RUN go mod download
COPY services/cattle-market-service/ .
RUN CGO_ENABLED=0 go build -o /bin/server ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /bin/server /server
EXPOSE 8087
ENTRYPOINT ["/server"]
