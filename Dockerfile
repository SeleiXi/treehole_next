FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN apk add --no-cache \
        ca-certificates \
        tzdata &&  \
    go mod download

COPY . .

RUN mkdir -p /root/.cache/go-buildtmp && \
    GOTMPDIR=/root/.cache/go-buildtmp CGO_ENABLED=0 go build -buildvcs=false -tags production -ldflags "-s -w" -o treehole

FROM alpine

WORKDIR /app

COPY --from=builder /app/treehole /app/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY data data

ENV TZ=Asia/Shanghai
ENV MODE=production

EXPOSE 8000

ENTRYPOINT ["./treehole"]
