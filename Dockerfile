FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
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
