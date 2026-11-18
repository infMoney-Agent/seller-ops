FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /screening .

FROM alpine:3.22
RUN apk add --no-cache ca-certificates && addgroup -g 10001 screening && adduser -D -u 10001 -G screening screening && mkdir /screening-data && chown screening:screening /screening-data
COPY --from=build /screening /usr/local/bin/screening
USER screening
EXPOSE 8090
ENTRYPOINT ["screening"]
CMD ["serve"]
HEALTHCHECK --interval=15s --timeout=5s --retries=5 CMD ["screening", "health"]
