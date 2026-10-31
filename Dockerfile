FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS build
WORKDIR /src
ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH
COPY go.mod ./
COPY . .
# Tests run in the native CI verification job; cross-compile here to avoid emulation.
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/everyoneagent .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/everyoneagent /everyoneagent
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/everyoneagent"]
