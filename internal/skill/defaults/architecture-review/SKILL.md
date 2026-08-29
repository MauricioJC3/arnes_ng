---
name: architecture-review
description: Reviewing a change against architectural boundaries — dependency direction, layer leaks, coupling, cohesion, blast radius, test isolation. Use when assessing whether a diff or a design respects the system's structure.
---

# Architecture Review

A focused pass over a change to catch structural regressions before they set.
This is not a style review — it is about whether the change keeps the system's
boundaries intact. Pairs with the `software-architecture` skill, which defines
the boundaries this skill checks.

## What to look at, in order

### 1. Dependency direction

- Does new domain / core code import infrastructure (DB drivers, HTTP clients,
  framework packages, environment access)? That is a dependency pointing the
  wrong way — flag it.
- Are new interfaces (ports) owned by the layer that **uses** them, not the layer
  that implements them?
- Did an outer concern (pagination params, HTTP status, ORM model) leak into a
  use case or entity signature?

### 2. Coupling and cohesion

- How many packages does the change touch to deliver one behavior? A single
  feature spread across many modules signals boundaries in the wrong place.
- Does the new code belong with the data and rules it operates on, or is it
  reaching across the codebase to assemble them?
- New shared/util/common code: is it genuinely generic, or domain logic hiding in
  a neutral-sounding package?

### 3. Blast radius

- What depends on the symbols this change modifies? Widen the review to the
  callers, not just the diff.
- Does a change to one adapter force changes in unrelated adapters? They should
  be isolated behind the port.
- Are public signatures changing in a way that ripples outward?

### 4. Test isolation

- Can the new logic be tested without real I/O — no database, no network, no
  clock, no filesystem?
- If a test now needs a container or a live service that it didn't before, a
  dependency crossed a boundary it shouldn't have.
- Are fakes/stubs substituting at a port, or is the test reaching into
  internals?

## Output of a review

Sort findings by severity:

- **Blocking** — dependency rule violated, a boundary erased, domain now coupled
  to a framework. The change makes the architecture worse.
- **Concern** — rising coupling, a util package growing, a leak that is contained
  but shouldn't spread. Worth fixing now, not a hard stop.
- **Note** — naming, placement, a smaller structural nit.

For each: name the boundary at risk, show the specific line or dependency, and
state the smallest change that restores the boundary.

## Anti-patterns that should always be raised

- Business rules inside a controller or HTTP handler.
- Database queries in a use case instead of behind a repository port.
- Entities importing the ORM, the web framework, or `os` / `env`.
- A `utils` / `helpers` / `common` package accumulating unrelated functions.
- Tests that boot the whole app to exercise one pure function.
