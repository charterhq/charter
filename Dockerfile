# Web UI: built here and embedded into agentd. NPM_REGISTRY points at a mirror where npmjs.org is closed.
FROM node:22-alpine AS web
WORKDIR /src/web
ARG NPM_REGISTRY=https://registry.npmjs.org/
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund --registry="${NPM_REGISTRY}"
COPY web/ ./
RUN mkdir -p ../internal/webui/dist && npm run build

FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/webui/dist ./internal/webui/dist
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/ ./cmd/...

# CLI for CI jobs (agentctl validate, publish): has a busybox shell, which CI runners need.
FROM gcr.io/distroless/static-debian12:debug-nonroot AS cli
COPY --from=build /out/agentctl /usr/local/bin/
ENTRYPOINT []

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/agentd /out/agentctl /usr/local/bin/
USER nonroot
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/agentd"]
CMD ["serve"]
