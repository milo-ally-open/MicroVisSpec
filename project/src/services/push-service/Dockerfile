FROM golang:1.27.0-bookworm AS build
WORKDIR /src
COPY go/go.mod ./go/go.mod
COPY go/go.sum ./go/go.sum
WORKDIR /src/go
RUN go mod download
COPY go/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/push-service ./cmd/push-service

FROM busybox:1.37.0-musl AS tools
FROM gcr.io/distroless/base-debian12:nonroot
COPY --from=build /out/push-service /push-service
COPY --from=tools /bin/busybox /busybox
EXPOSE 8080
ENTRYPOINT ["/push-service"]
