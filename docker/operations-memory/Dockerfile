FROM golang:1.24-bookworm AS build

WORKDIR /src

COPY ./go.mod ./go.sum ./
RUN go mod download

COPY ./ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/operations-memory-go ./cmd/operations-memory-go

FROM alpine:3.20

RUN apk add --no-cache ca-certificates

COPY --from=build /out/operations-memory-go /usr/local/bin/operations-memory-go
WORKDIR /app
COPY openapi.json /app/openapi.json

EXPOSE 8888
ENTRYPOINT ["/usr/local/bin/operations-memory-go"]
