# Builds cmd/server only. Migrations run as a separate one-off step
# (`go run ./cmd/migrate up`) against the target database before a first
# deploy — not baked into container startup, so a container restart never
# re-runs migrations by accident.
#
# Referenced by ../infra/app.yaml (source_dir: api, so this file's build
# context is the api/ directory — the repo-root .env never enters it).
#
# The golang:1.26-alpine tag below matches go.mod's `go 1.26.6` toolchain
# line as of when this was written; bump it if go.mod's version has moved
# on since.

FROM golang:1.26-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/server ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /out/server /server

EXPOSE 8080
ENTRYPOINT ["/server"]
