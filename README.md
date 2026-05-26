# Code Share

A lightweight real-time code sharing and collaboration app built with Go, Redis, HTMX, Alpine.js, Tailwind CSS, and Monaco Editor.

## Features

- Instant unique room URLs with Redis-backed session records
- Pessimistic locking so only one user edits at a time
- Monaco Editor with multi-language support
- HTMX-powered formatting endpoint with native error handling

## Run locally

1. Build and run with Docker Compose:

```bash
./startup.sh
```

2. Open `http://localhost:8080` in the browser.

## Redis

The app connects to Redis at `localhost:6379` by default. Change the address with `REDIS_ADDR`.

## Project files

- `main.go` — HTTP server, routing, templates, Redis session handling, formatting endpoint
- `hub.go` — WebSocket room and lock management
- `templates/index.html` — Dark-themed UI and Monaco integration
- `Dockerfile` — Multi-stage Go build
- `docker-compose.yml` — Go app + Redis stack
