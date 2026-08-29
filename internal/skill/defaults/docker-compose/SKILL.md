---
name: docker-compose
description: Defining multi-service local and CI environments with Compose — service dependencies, healthchecks, networks, named volumes, profiles, env handling. Use when writing or debugging a compose.yaml.
---

# Docker Compose

Compose describes a set of services that run together. Use it for local
development, integration tests, and CI — not as a production orchestrator.

## A shape that works

```yaml
services:
  api:
    build: .
    ports:
      - "3000:3000"
    environment:
      DATABASE_URL: postgres://app:app@db:5432/app
    depends_on:
      db:
        condition: service_healthy
    develop:
      watch:
        - path: ./src
          action: sync
          target: /app/src

  db:
    image: postgres:17
    environment:
      POSTGRES_USER: app
      POSTGRES_PASSWORD: app
      POSTGRES_DB: app
    volumes:
      - db-data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U app"]
      interval: 5s
      timeout: 3s
      retries: 5

volumes:
  db-data:
```

## `depends_on` needs a condition

Plain `depends_on: [db]` only waits for the container to **start**, not for the
database to accept connections. Pair it with a `healthcheck` on the dependency
and `condition: service_healthy`, or your app races the database on every boot.

## Networks

- Compose creates one default network; services reach each other by **service
  name** as hostname (`db:5432`), not `localhost`.
- `localhost` inside a container is that container. Only the host uses the
  published `ports:` mapping.
- Split networks (e.g. `frontend` / `backend`) when you want to keep the database
  off the public-facing segment.

## Volumes: named vs bind

| Kind | Syntax | Use for |
|------|--------|---------|
| Named volume | `db-data:/var/lib/...` | Persistent state Docker manages (databases). |
| Bind mount | `./src:/app/src` | Live source code from the host during development. |

Never bind-mount over a path the image needs to populate (like
`node_modules`) — the host directory hides the image's contents.

## Profiles keep one file for many jobs

```yaml
services:
  worker:
    profiles: ["full"]
```

`docker compose up` skips it; `docker compose --profile full up` includes it.
Good for optional workers, seed jobs, or a Playwright container.

## Environment

- `environment:` sets values explicitly. `env_file:` loads a file.
- Compose auto-reads a `.env` file **for variable substitution in the YAML
  itself** — that is a separate mechanism from `env_file:`.
- Keep real secrets out of the committed compose file; use `.env` (gitignored) or
  `secrets:`.

## Before you call it done

- `docker compose config` renders without warnings.
- `docker compose up` from clean brings every service to healthy.
- App connects to dependencies by service name, and retries a not-yet-ready
  dependency instead of crashing.
