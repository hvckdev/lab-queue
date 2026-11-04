FROM golang:1.25-alpine AS build
ENV GOWORK=off CGO_ENABLED=0
WORKDIR /src/projects/accounts
COPY projects/accounts/go.mod projects/accounts/go.sum ./
COPY pkg/platform/go.mod pkg/platform/go.sum /src/pkg/platform/
RUN go mod download
COPY pkg/platform /src/pkg/platform
COPY projects/accounts ./
RUN go build -trimpath -ldflags="-s -w" -o /out/server ./cmd \
    && go build -trimpath -ldflags="-s -w" -o /out/healthcheck labqueue/pkg/platform/cmd/healthcheck

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=build /out/server /app/server
COPY --from=build /out/healthcheck /app/healthcheck
USER 65532:65532
EXPOSE 8080
HEALTHCHECK --interval=5s --timeout=3s --start-period=20s --retries=10 CMD ["/app/healthcheck"]
ENTRYPOINT ["/app/server"]
