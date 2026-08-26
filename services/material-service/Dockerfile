FROM golang:1.26-alpine AS builder
WORKDIR /workspace
COPY pkg/ ./pkg/
COPY libs/ ./libs/
COPY services/material-service/go.mod services/material-service/go.sum* ./services/material-service/
WORKDIR /workspace/services/material-service
RUN go mod download
COPY services/material-service/ .
RUN CGO_ENABLED=0 go build -o /bin/server ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /bin/server /server
EXPOSE 8103
ENTRYPOINT ["/server"]
