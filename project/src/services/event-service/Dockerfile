FROM golang:1.27.0-bookworm AS build
WORKDIR /src
COPY go/go.mod ./go/go.mod
COPY go/go.sum ./go/go.sum
WORKDIR /src/go
RUN go mod download
COPY go/ ./
RUN mkdir -p /out/evidence && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/event-service ./cmd/event-service

FROM busybox:1.37.0-musl AS tools
FROM gcr.io/distroless/base-debian12:nonroot
COPY --from=build /out/event-service /event-service
COPY --from=build --chown=nonroot:nonroot /out/evidence /var/lib/yunshao/evidence
COPY --from=tools /bin/busybox /busybox
EXPOSE 8080
ENTRYPOINT ["/event-service"]
