FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o reporting-service ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /app/reporting-service /reporting-service
EXPOSE 8096
ENTRYPOINT ["/reporting-service"]
