# SPDX-License-Identifier: AGPL-3.0-only
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/openwebui-tools .

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=build /out/openwebui-tools /usr/local/bin/openwebui-tools
ENV ADDR=:8090
EXPOSE 8090
HEALTHCHECK CMD wget -qO- http://127.0.0.1:8090/health >/dev/null || exit 1
ENTRYPOINT ["openwebui-tools"]
