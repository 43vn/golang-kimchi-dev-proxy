FROM golang:1.25-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
COPY main.go ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go build -o /app/server .

FROM alpine:3.21

RUN apk add --no-cache ca-certificates

COPY --from=builder /app/server /server

EXPOSE 8080
ENTRYPOINT ["/server"]
