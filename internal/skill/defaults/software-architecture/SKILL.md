---
name: software-architecture
description: Designing and structuring code with Clean / Hexagonal / Screaming Architecture — the dependency rule, ports and adapters, keeping domain logic free of frameworks, naming by domain. Use when laying out a module, deciding where code belongs, or reviewing structure.
---

# Software Architecture

The goal is a codebase where the important decisions are easy to change and the
boring details (framework, database, transport) are easy to replace. That comes
from **one rule about which way dependencies point**.

## The dependency rule

Source code dependencies point **inward**, toward the domain. Nothing in the
domain layer knows about the web framework, the ORM, the message broker, or the
JSON shape of a request.

```
  HTTP / CLI / gRPC handlers   ─┐
  DB repositories, API clients ─┼─►  Use cases  ─►  Domain entities
  framework glue               ─┘   (application)     (pure business rules)
        adapters (outer)              inner ────────────────►
```

- **Domain / entities**: business concepts and invariants. Plain types. No
  imports from outer layers.
- **Application / use cases**: orchestrates entities to fulfil one operation.
  Depends on the domain and on **ports** (interfaces), never on concrete
  infrastructure.
- **Adapters**: implement the ports — a Postgres repository, an HTTP handler, an
  S3 client. This is where framework code lives.

If you can't unit-test a use case without spinning up a database or an HTTP
server, a dependency is pointing the wrong way.

## Ports and adapters

The use case declares the interface it needs; the adapter satisfies it. The
interface is owned by the **inner** layer.

```go
// application layer — owns the port
type OrderRepository interface {
    Save(ctx context.Context, o Order) error
    ByID(ctx context.Context, id OrderID) (Order, error)
}

// adapter layer — depends inward to implement it
type PostgresOrderRepository struct { db *sql.DB }
func (r PostgresOrderRepository) Save(ctx context.Context, o domain.Order) error { ... }
```

Swapping Postgres for an in-memory fake in tests, or for a different store in
production, touches only the adapter.

## Screaming Architecture

The top-level package layout should tell you what the system **does**, not what
framework it uses. `orders/`, `billing/`, `shipping/` — not `controllers/`,
`models/`, `services/`. A newcomer should read the directory listing and guess
the business.

## Naming

- Reject `utils`, `helpers`, `common`, `shared`, `manager`, `misc`. They are
  where unrelated code goes to rot.
- Name by role in the domain: `PriceCalculator`, `InvoiceNumberGenerator`,
  `PaymentAuthorizer`.
- One package, one reason to exist.

## Keep the units small

- Functions do one thing; prefer early returns over nested conditionals.
- Split a file once it holds more than one clear responsibility.
- Duplication is cheaper than the wrong abstraction — wait until the third
  occurrence before extracting.

## When reviewing structure, ask

- Does domain code import a framework, a driver, or a transport package? (It
  shouldn't.)
- Can each use case be tested with fakes, no I/O?
- Do the package names describe the business or the plumbing?
- Is there a `utils` package quietly accumulating everything?
