FROM golang:1.25 AS build-stage

# from Makefile
ARG GITCOMMIT

# sqlite3 backend does not work on distroless images
# only redis/valkey is be available
ENV CGO_ENABLED=0

WORKDIR /build

COPY ./app/ app/
COPY ./cmd/ cmd/
COPY go.mod .
COPY go.sum .

RUN go build -tags prod -ldflags "-s -w -X main.version=${GITCOMMIT}" -o target/ ./cmd/...

FROM gcr.io/distroless/static-debian12

COPY --from=build-stage /build/target/goldfish /app/goldfish

# sqlite3 backend does not work on distroless images
# only redis/valkey is be available
ENV BACKEND_STORE=redis

# PID files are not useful in containers
ENV PID_FILE=skip

CMD ["/app/goldfish"]
