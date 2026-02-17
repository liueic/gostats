FROM --platform=$BUILDPLATFORM golang:1.22-alpine AS builder

WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags="-s -w" -o /out/gostats ./cmd/gostats

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /
COPY --from=builder /out/gostats /gostats

ENV PORT=8080
ENV CONFIG_FILE=/config.yml
EXPOSE 8080

ENTRYPOINT ["/gostats"]
