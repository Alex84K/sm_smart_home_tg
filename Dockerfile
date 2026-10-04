# syntax=docker/dockerfile:1

FROM golang:alpine AS builder
ARG GOPRIVATE=github.com/Alex84K/*
ENV GOPRIVATE=${GOPRIVATE}
WORKDIR /src/tg_gateway_go

# Copy contract from named build context (BuildKit additional_contexts: contract)
# placed next to tg_gateway_go to match the replace directive ../core_syst_go/contract
COPY --from=contract . /src/core_syst_go/contract

COPY go.mod go.sum ./
RUN --mount=type=secret,id=netrc,target=/root/.netrc,required=false go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/tg-gateway ./cmd/tg-gateway

FROM alpine:latest
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /out/tg-gateway /app/tg-gateway
ENTRYPOINT ["/app/tg-gateway"]
