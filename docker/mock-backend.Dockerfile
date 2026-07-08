FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o mock-backend ./cmd/mock-backend

FROM alpine:3.20
WORKDIR /app
COPY --from=builder /app/mock-backend .
EXPOSE 8081
CMD ["./mock-backend"]
