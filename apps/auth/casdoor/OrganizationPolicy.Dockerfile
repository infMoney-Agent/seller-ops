# Build context: repository root. Keep the upstream revision and patch together.
FROM golang:1.26.10 AS source
WORKDIR /src
RUN git init && git remote add origin https://github.com/casdoor/casdoor.git \
    && git fetch --depth=1 origin 528a0213f01caf70b9f41009cbf7b2f67bdd67ec \
    && git checkout --detach FETCH_HEAD
COPY apps/auth/casdoor/organization-policy.patch /tmp/organization-policy.patch
RUN git apply --check /tmp/organization-policy.patch && git apply /tmp/organization-policy.patch

FROM node:20.20.1 AS frontend
WORKDIR /web
COPY --from=source /src/web/package.json /src/web/yarn.lock ./
RUN yarn install --frozen-lockfile --network-timeout 1000000
COPY --from=source /src/web ./
RUN ./node_modules/.bin/esbuild tests/organization-identity.test.mjs \
      --bundle --platform=node --format=esm --outfile=/tmp/organization-identity.test.mjs \
    && node --test /tmp/organization-identity.test.mjs \
    && node node_modules/typescript/bin/tsc --noEmit \
    && NODE_OPTIONS=--max-old-space-size=3072 yarn run build

FROM source AS backend
ENV CGO_ENABLED=0
RUN go test object/organization_identity.go object/organization_identity_test.go \
    && go build -trimpath -ldflags="-w -s" -o /tmp/server .

FROM casbin/casdoor:v4.25.0@sha256:a63e7a78257c43c90b22a49bf93becca7007dc47b4f97bec763aa50bcc9c9ee9
COPY --from=backend --chown=1000:1000 /tmp/server /server
COPY --from=frontend --chown=1000:1000 /web/build /web/build
