FROM golang:1.23-bookworm AS build

WORKDIR /src

COPY go/go.mod go/go.sum ./
RUN go mod download

COPY go/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/singularity-memory-go ./cmd/singularity-memory-go

FROM alpine:3.20

RUN apk add --no-cache ca-certificates

COPY --from=build /out/singularity-memory-go /usr/local/bin/singularity-memory-go
WORKDIR /app
COPY openapi.json /app/openapi.json

EXPOSE 8888
ENTRYPOINT ["/usr/local/bin/singularity-memory-go"]
