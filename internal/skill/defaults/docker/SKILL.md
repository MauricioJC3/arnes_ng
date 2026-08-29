---
name: docker
description: Writing and reviewing Dockerfiles — multi-stage builds, layer-cache ordering, small and secure images, non-root runtime, healthchecks. Use when authoring a Dockerfile, shrinking an image, or hardening a container.
---

# Docker

Build images that are small, cacheable, reproducible, and safe to run in
production. This skill is about the **Dockerfile and the image**; local
multi-service orchestration lives in the `docker-compose` skill.

## Order instructions by how often they change

Docker caches each instruction as a layer and invalidates every layer after the
first change. Put the stable things first, the volatile things last.

```dockerfile
FROM node:22-slim AS base
WORKDIR /app

# Dependencies: changes only when the lockfile changes.
COPY package.json package-lock.json ./
RUN npm ci

# Source: changes on every commit — keep it after the dependency layer.
COPY . .
RUN npm run build
```

Copying `. .` before installing dependencies is the most common mistake: every
source edit then re-runs the install.

## Multi-stage: build fat, ship thin

The final stage should contain the runtime and the built artifact — nothing
else. No compilers, no dev dependencies, no build cache.

```dockerfile
FROM node:22 AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build && npm prune --omit=dev

FROM node:22-slim AS runtime
WORKDIR /app
ENV NODE_ENV=production
COPY --from=build /app/node_modules ./node_modules
COPY --from=build /app/dist ./dist
USER node
EXPOSE 3000
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD node -e "fetch('http://localhost:3000/health').then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))"
CMD ["node", "dist/index.js"]
```

## Base image, smallest that still works

| Choice | When |
|--------|------|
| `-slim` (Debian) | Default. glibc, small, few surprises. |
| `-alpine` | Only if you have tested it — musl breaks some native modules and DNS edge cases. |
| `distroless` / `scratch` | Static binaries (Go, Rust). No shell, minimal attack surface. |

Pin a real tag (`node:22.11-slim`), never rely on the meaning of `latest` staying
constant.

## Security baseline

- **Run as non-root.** Create or reuse an unprivileged user and `USER` it before
  `CMD`. A container breakout as root is a host root.
- **No secrets in layers.** `ENV`, `ARG`, and `COPY`ed files persist in the image
  history. Use build secrets (`RUN --mount=type=secret`) or inject at runtime.
- **`.dockerignore`** is mandatory: exclude `.git`, `node_modules`, local env
  files, build output. It shrinks the build context and stops secrets leaking in.
- **Drop capabilities and set `--read-only`** at run time where the workload
  allows it.

## Before you call it done

- `docker build` from a clean context succeeds and the image runs.
- `docker history <image>` shows no secret values and no surprise bloat.
- Final image size is in line with the base — a slim Node app is tens of MB, not
  hundreds.
- The container starts as a non-root user (`docker run --rm <image> id`).
