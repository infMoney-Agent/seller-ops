FROM golang:1.25-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /ecom .

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates chromium fonts-noto-cjk && rm -rf /var/lib/apt/lists/* && useradd -m -u 10001 ecom
COPY --from=build /ecom /usr/local/bin/ecom
ENV CHROME_BIN=/usr/bin/chromium
USER ecom
EXPOSE 8000
ENTRYPOINT ["ecom"]
CMD ["serve"]
HEALTHCHECK --interval=15s --timeout=5s --retries=5 CMD ["ecom", "health"]
