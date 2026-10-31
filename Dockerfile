FROM golang:1.25-alpine AS build
WORKDIR /src
ARG VERSION=dev
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go test ./... && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/everyoneagent .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/everyoneagent /everyoneagent
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/everyoneagent"]
