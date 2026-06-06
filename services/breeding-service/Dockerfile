FROM golang:1.26-alpine AS builder
WORKDIR /workspace
COPY services/breeding-service/go.mod services/breeding-service/go.sum* ./services/breeding-service/
COPY pkg/ ./pkg/
WORKDIR /workspace/services/breeding-service
RUN go mod download
COPY services/breeding-service/ .
RUN CGO_ENABLED=0 go build -o /bin/server ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /bin/server /server
EXPOSE 8080
ENTRYPOINT ["/server"]
