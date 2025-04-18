FROM golang:1.24 AS build-stage

# from Makefile
ARG GITCOMMIT

# sqlite3 backend will not work
# only redis/valkey will be available
ENV CGO_ENABLED=0

WORKDIR /build

COPY ./app/ app/
COPY ./cmd/ cmd/
COPY go.mod .
COPY go.sum .

RUN go build -tags prod -ldflags "-s -w -X main.version=${GITCOMMIT}" -o target/ ./cmd/...

FROM gcr.io/distroless/static-debian12

COPY --from=build-stage /build/target/goldfish /app/goldfish

CMD ["/app/goldfish"]
