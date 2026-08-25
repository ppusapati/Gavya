FROM golang:1.26-alpine AS builder
WORKDIR /workspace
COPY services/billing-service/go.mod services/billing-service/go.sum* ./services/billing-service/
COPY pkg/ ./pkg/
COPY libs/ ./libs/
WORKDIR /workspace/services/billing-service
RUN go mod download
COPY services/billing-service/ .
RUN CGO_ENABLED=0 go build -o /bin/server ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /bin/server /server
EXPOSE 8097
ENTRYPOINT ["/server"]
