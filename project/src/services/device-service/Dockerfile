FROM golang:1.27.0-bookworm AS build
WORKDIR /src
COPY go/go.mod go/go.sum ./
RUN go mod download
COPY go/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/device-service ./cmd/device-service

FROM busybox:1.37.0-musl AS tools
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/device-service /device-service
COPY --from=tools /bin/busybox /busybox
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/device-service"]
