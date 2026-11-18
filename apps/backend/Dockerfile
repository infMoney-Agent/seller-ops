FROM golang:1.25-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /seller-backend .

FROM debian:bookworm-slim
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates chromium fonts-noto-cjk && rm -rf /var/lib/apt/lists/* && useradd -m -u 10001 seller && mkdir /screening-data && chown seller:seller /screening-data
COPY --from=build /seller-backend /usr/local/bin/seller-backend
ENV CHROME_BIN=/usr/bin/chromium BACKEND_ADDR=:8000
USER seller
EXPOSE 8000
ENTRYPOINT ["seller-backend"]
CMD ["serve"]
HEALTHCHECK --interval=15s --timeout=5s --retries=5 CMD ["seller-backend", "health"]
