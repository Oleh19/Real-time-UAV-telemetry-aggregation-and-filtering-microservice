FROM golang:1.26.5 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/server ./cmd/server \
 && CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/simulator ./cmd/simulator \
 && CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/geofence ./cmd/geofence \
 && CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/notifier ./cmd/notifier \
 && CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/fleet ./cmd/fleet \
 && CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/certgen ./cmd/certgen

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /usr/local/bin/server
COPY --from=build /out/simulator /usr/local/bin/simulator
COPY --from=build /out/geofence /usr/local/bin/geofence
COPY --from=build /out/notifier /usr/local/bin/notifier
COPY --from=build /out/fleet /usr/local/bin/fleet
COPY --from=build /out/certgen /usr/local/bin/certgen
USER nonroot
ENTRYPOINT ["/usr/local/bin/server"]
