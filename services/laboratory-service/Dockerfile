FROM golang:1.26-alpine AS builder
WORKDIR /workspace
COPY pkg/ ./pkg/
COPY libs/ ./libs/
COPY services/laboratory-service/go.mod services/laboratory-service/go.sum* ./services/laboratory-service/
WORKDIR /workspace/services/laboratory-service
RUN go mod download
COPY services/laboratory-service/ .
RUN CGO_ENABLED=0 go build -o /bin/server ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /bin/server /server
EXPOSE 8103
ENTRYPOINT ["/server"]
