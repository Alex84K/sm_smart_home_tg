# syntax=docker/dockerfile:1

FROM golang:alpine AS builder
RUN apk add --no-cache git
ARG GOPRIVATE=github.com/Alex84K/*
ENV GOPRIVATE=${GOPRIVATE}
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=secret,id=netrc,target=/root/.netrc,required=false go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/tg-gateway ./cmd/tg-gateway

FROM alpine:latest
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /out/tg-gateway /app/tg-gateway
ENTRYPOINT ["/app/tg-gateway"]
