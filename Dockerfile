FROM golang:1.22 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/server ./cmd/server \
 && CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/simulator ./cmd/simulator \
 && CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/geofence ./cmd/geofence

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /usr/local/bin/server
COPY --from=build /out/simulator /usr/local/bin/simulator
COPY --from=build /out/geofence /usr/local/bin/geofence
USER nonroot
ENTRYPOINT ["/usr/local/bin/server"]
