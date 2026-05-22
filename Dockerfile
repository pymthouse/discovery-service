# syntax=docker/dockerfile:1

FROM golang:1.22-alpine AS build
RUN apk add --no-cache git ca-certificates
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /discoveryd ./cmd/discoveryd

FROM alpine:3.23
RUN apk add --no-cache ca-certificates wget
WORKDIR /app
COPY --from=build /discoveryd /app/discoveryd

EXPOSE 8088
ENTRYPOINT ["/app/discoveryd"]
