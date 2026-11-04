FROM node:24-alpine AS frontend
WORKDIR /src/frontend
COPY projects/web/frontend/package.json projects/web/frontend/package-lock.json ./
RUN npm ci
COPY projects/web/frontend ./
RUN npm run build -- --outDir /out

FROM golang:1.25-alpine AS build
ENV GOWORK=off CGO_ENABLED=0
WORKDIR /src/projects/web
COPY projects/web/go.mod projects/web/go.sum ./
COPY pkg/platform/go.mod pkg/platform/go.sum /src/pkg/platform/
RUN go mod download
COPY pkg/platform /src/pkg/platform
COPY projects/web ./
RUN rm -rf internal/assets
COPY --from=frontend /out ./internal/assets
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
