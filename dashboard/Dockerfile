FROM --platform=$BUILDPLATFORM node:24-alpine AS frontend
WORKDIR /src/dashboard
COPY dashboard/package.json dashboard/package-lock.json ./
RUN npm ci
COPY dashboard/index.html dashboard/tsconfig.json dashboard/vite.config.ts ./
COPY dashboard/src ./src
COPY dashboard/server ./server
COPY dashboard/public ./public
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS backend
ARG TARGETOS=linux
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ak3s-dashboard ./cmd/ak3s-dashboard
COPY internal ./internal
COPY platform ./platform
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags='-s -w' -o /ak3s-dashboard ./cmd/ak3s-dashboard

FROM scratch
COPY --from=backend /ak3s-dashboard /ak3s-dashboard
COPY --from=frontend /src/dashboard/dist /app/dashboard
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/ak3s-dashboard"]
