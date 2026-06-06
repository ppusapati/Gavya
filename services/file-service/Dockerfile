FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o file-service ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /app/file-service /file-service
EXPOSE 8095
ENTRYPOINT ["/file-service"]
