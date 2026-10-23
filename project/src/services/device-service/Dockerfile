FROM golang:1.27.0-bookworm AS build
WORKDIR /src
COPY services/device-service/go/go.mod services/device-service/go/go.sum ./
RUN go mod download
COPY services/device-service/go/ ./
COPY device_sides/atlas200idka2/ /atlas200idka2/
RUN mkdir -p /out && tar -czf /out/atlas200idka2-bootstrap.tar.gz -C /atlas200idka2 .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/device-service ./cmd/device-service

FROM busybox:1.37.0-musl AS tools
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/device-service /device-service
COPY --from=build /out/atlas200idka2-bootstrap.tar.gz /opt/yunshao/atlas200idka2-bootstrap.tar.gz
COPY --from=tools /bin/busybox /busybox
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/device-service"]
