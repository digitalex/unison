# Unison server image (Cloud Run). The web UI is prebuilt and embedded from
# internal/webui/dist, so only Go is needed. Both SQL drivers are pure Go.
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /unison ./cmd/unison

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /unison /unison
# Cloud Run sets PORT; `serve` listens on :$PORT when it is set. Set UNISON_DB
# to a Postgres DSN (the default SQLite path is not persistent on Cloud Run).
ENV PORT=8080
EXPOSE 8080
ENTRYPOINT ["/unison", "serve"]
