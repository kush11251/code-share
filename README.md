# Code Share

A lightweight real-time code sharing and collaboration app built with Go, Redis, Docker, and a simple local JavaScript frontend.

## Features

- Instant unique room URLs with Redis-backed session records
- Pessimistic locking so only one user edits at a time
- Live room control, shared text updates, and code formatting
- Works with local Docker Compose and Render-managed Redis

## Run locally

1. Build and run with Docker Compose:

```bash
./startup.sh
```

2. Open `http://localhost:8080` in the browser.

## Redis

The app supports Redis using either:

- `REDIS_ADDR` = `host:port`
- `REDIS_HOST` + `REDIS_PORT`
- `REDIS_URL` = `redis://...`
- `REDIS_PASSWORD` for password-protected Redis

For Docker Compose, the app already uses:

```yaml
REDIS_ADDR=redis:6379
```

For Render managed Redis, set:

```bash
REDIS_HOST=${REDIS_HOST}
REDIS_PORT=${REDIS_PORT}
REDIS_PASSWORD=${REDIS_PASSWORD}
```

Or simply set:

```bash
REDIS_URL=redis://:<password>@<host>:<port>
```

## Project files

- `main.go` — HTTP server, routing, templates, Redis session handling, formatting endpoint
- `hub.go` — WebSocket room and lock management
- `templates/index.html` — Dark-themed UI and Monaco integration
- `Dockerfile` — Multi-stage Go build
- `docker-compose.yml` — Go app + Redis stack
