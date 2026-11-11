FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS build
WORKDIR /src
ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH
COPY go.mod ./
COPY . .
RUN mkdir -p /out/data && chown 65532:65532 /out/data
# Tests run in the native CI verification job; cross-compile here to avoid emulation.
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/agent .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/agent /agent
# Keep existing deployments' health checks working during the executable rename.
COPY --from=build /out/agent /everyoneagent
COPY --from=build --chown=65532:65532 /out/data /data
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/agent"]
