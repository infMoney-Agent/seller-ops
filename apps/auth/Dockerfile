FROM golang:1.25.3-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go schema.sql ./
COPY internal/ ./internal/
RUN CGO_ENABLED=0 go build -trimpath -o /seller-server .

FROM alpine:3.22.2
RUN apk add --no-cache ca-certificates
COPY --from=build /seller-server /usr/local/bin/seller-server
USER 65532:65532
ENTRYPOINT ["seller-server"]
