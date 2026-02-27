FROM golang:1.24-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o /out/mergen-xds-center ./cmd/mergen-xds-center

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=builder /out/mergen-xds-center /usr/local/bin/mergen-xds-center

ENTRYPOINT ["/usr/local/bin/mergen-xds-center"]
CMD ["serve"]
