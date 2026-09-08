FROM golang:1.25-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
        -o /out/api ./cmd/api \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
        -o /out/restaurant-sample ./cmd/restaurant-sample \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
        -o /out/migrate ./cmd/migrate

FROM alpine:3.20 AS runtime
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 app
WORKDIR /app
COPY --from=builder /out/api /app/api
COPY --from=builder /out/restaurant-sample /app/restaurant-sample
COPY --from=builder /out/migrate /app/migrate
COPY api/openapi.yaml /app/openapi.yaml
COPY migrations /app/migrations
USER app
EXPOSE 8080
CMD ["/app/api"]
