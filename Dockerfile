# syntax=docker/dockerfile:1
FROM golang:1.22-alpine AS builder
WORKDIR /app

COPY go.mod .
RUN go env -w GOPROXY=https://proxy.golang.org,direct
RUN go mod download

COPY . .
RUN go build -o /usr/local/bin/codeshare main.go hub.go

FROM alpine:latest
RUN apk add --no-cache ca-certificates
COPY --from=builder /usr/local/bin/codeshare /usr/local/bin/codeshare
COPY templates /app/templates
WORKDIR /app
EXPOSE 8080
ENV ADDR=:8080
CMD ["/usr/local/bin/codeshare"]
