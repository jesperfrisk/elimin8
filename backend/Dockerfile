FROM golang:1.27-alpine AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG BUILD_REVISION=unknown
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X github.com/jesperfrisk/elimin8/backend/internal/buildinfo.revision=${BUILD_REVISION}" -o /api ./cmd/api

FROM alpine:3
RUN apk add --no-cache ca-certificates && addgroup -S app && adduser -S -G app app
WORKDIR /app
COPY --from=build /api ./api
USER app
EXPOSE 8080
ENTRYPOINT ["/app/api"]
