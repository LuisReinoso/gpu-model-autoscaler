FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY go.mod go.sum* ./
RUN go mod download 2>/dev/null || true
COPY *.go ./
RUN CGO_ENABLED=0 go build -o proxy .

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=builder /app/proxy /proxy
EXPOSE 8080
CMD ["/proxy"]