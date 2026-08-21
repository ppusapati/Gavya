FROM golang:1.26-alpine AS builder
WORKDIR /workspace
COPY pkg/ ./pkg/
COPY libs/ ./libs/
COPY services/observation-service/go.mod services/observation-service/go.sum* ./services/observation-service/
WORKDIR /workspace/services/observation-service
RUN go mod download
COPY services/observation-service/ .
RUN CGO_ENABLED=0 go build -o /bin/server ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /bin/server /server
EXPOSE 8092
ENTRYPOINT ["/server"]
