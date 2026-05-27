FROM golang:1.21-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /out/p2pbk ./cmd/p2pbk && \
    go build -o /out/backupd ./cmd/backupd && \
    go build -o /out/bootstrap ./cmd/bootstrap

FROM alpine:3.20
RUN adduser -D -H p2pbk
COPY --from=build /out/p2pbk /usr/local/bin/p2pbk
COPY --from=build /out/backupd /usr/local/bin/backupd
COPY --from=build /out/bootstrap /usr/local/bin/bootstrap
USER p2pbk
ENTRYPOINT ["/usr/local/bin/backupd"]
