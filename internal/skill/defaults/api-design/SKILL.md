---
name: api-design
description: Designing an HTTP/REST API — resource and route modelling, verbs and status codes, versioning, pagination, a consistent error contract, and the middleware chain (recovery, logging, CORS, rate limit, auth, validation) with request-scoped context passing between layers. Use when adding an endpoint, wiring middleware, or connecting auth to handlers.
---

# API Design

A good API is a **contract**: predictable resources, predictable errors, and one
clear path a request takes from the socket to the business rule. Most bugs at
this layer come from the middleware chain and the handler disagreeing about what
is already validated, who the caller is, and what shape an error has.

## Resources and routes

- Model **nouns**, plural, kebab-case: `/v1/invoices`, `/v1/invoices/{id}/lines`.
- Verbs live in the HTTP method, not the path. `POST /invoices`, not
  `/invoices/create`. Reserve a verb-y sub-path only for true non-CRUD actions:
  `POST /invoices/{id}/send`.
- Nest one level to express ownership; deeper than that, use query filters:
  `GET /invoices?customer={id}` beats `/customers/{id}/invoices/{id}/lines/...`.
- Method semantics: `GET` safe and cacheable, `PUT`/`DELETE` idempotent, `POST`
  not. `PATCH` for partial updates.

| Outcome | Status |
|---------|--------|
| Created a resource | 201 + `Location` |
| Success, no body | 204 |
| Bad input shape / failed validation | 400 (or 422) |
| No/!invalid credentials | 401 |
| Authenticated but not allowed | 403 |
| Missing resource | 404 |
| Conflict / duplicate / version mismatch | 409 |
| Unhandled error | 500 (never leak internals) |

## Versioning and evolution

- Version in the path (`/v1/`) or an `Accept` header — pick one, apply it
  everywhere.
- Additive changes (new optional field, new endpoint) don't bump the version.
  Removing a field, renaming, or tightening validation does.

## Pagination, filtering, sorting

- Paginate every list endpoint from day one. Cursor-based for large or
  fast-changing sets; offset only for small stable ones.
- Return the page plus metadata (`next` cursor or `total`), never a bare array —
  a bare array can't grow a envelope later without breaking clients.

## The error contract

One shape for every error the API returns, ideally `application/problem+json`:

```json
{ "type": "about:blank", "title": "validation_failed", "status": 400,
  "detail": "email is required", "errors": [{ "field": "email", "rule": "required" }] }
```

- The handler maps a domain error to a status + this body. Nothing downstream
  invents its own format.
- Never return a framework stack trace or a raw driver error to the client. Log
  the detail, return the contract.

## The middleware chain

Order matters — each layer assumes the ones before it already ran:

```
recover → request-id → logging → CORS → rate-limit → auth → authz → body-validate → handler → use case
```

- **recover** outermost, so a panic anywhere still becomes a 500 in the error
  contract.
- **rate-limit before auth** for unauthenticated abuse; a second per-principal
  limit can sit after auth.
- **auth before authz before validation**: don't spend work parsing a body for a
  caller you'll reject with 401.

### Passing data between layers

- Use the **request-scoped context**, never package globals or mutable request
  fields. One typed key per concern:

```go
type principalKey struct{}

func withPrincipal(ctx context.Context, p Principal) context.Context {
    return context.WithValue(ctx, principalKey{}, p)
}
func PrincipalFrom(ctx context.Context) (Principal, bool) {
    p, ok := ctx.Value(principalKey{}).(Principal)
    return p, ok
}
```

- The **auth** middleware is the *only* writer of the principal. Every handler
  and every downstream middleware reads it through `PrincipalFrom` — a single
  source of truth. If a handler re-parses the token, you now have two.
- A middleware that can't establish its precondition **fails closed**: return the
  error contract and stop the chain, don't call the next handler with a zero
  value.

## Auth: authentication vs authorization

Two different questions, two different layers:

- **Authentication (authn)** — *who is calling?* Middleware validates the
  credential (JWT, session, API key), builds a `Principal` (id, scopes, tenant),
  puts it in context. Failure → **401**.
- **Authorization (authz)** — *may this caller do this?* Checked against the
  principal + the target resource. A coarse check (required scope/role) can be
  middleware; anything that needs the resource loaded ("owns this invoice") is a
  **use-case** decision, not middleware. Failure → **403**.

Keep authz rules out of the transport layer when they depend on domain state —
the use case has the data and is unit-testable without HTTP.

## Validation: shape vs rules

- **Transport validation** (in the adapter/handler): is the JSON well-formed, are
  required fields present, do types parse, is the enum in range. Reject with 400
  before building a domain object.
- **Business-rule validation** (in the domain): can this account be debited, is
  the invoice already sent. Lives with the entity/use case and returns a domain
  error the handler maps to 409/422.
- Map the request DTO to a domain type at the boundary. The use case never sees
  the raw request struct, so it can't accidentally depend on transport concerns.

## Anti-patterns

- Re-decoding the auth token inside a handler instead of reading the principal
  from context.
- Middleware that swallows its failure and calls `next` with an empty principal —
  the handler then 500s or, worse, treats the request as anonymous-but-allowed.
- Business authorization ("is owner") implemented as route middleware that has to
  reach into the database itself.
- Each handler formatting errors its own way; clients can't parse failures
  uniformly.
- Returning `200` with `{ "error": ... }` in the body.
- Validation logic duplicated in the handler and the domain, drifting apart.
- List endpoints returning a bare JSON array with no pagination envelope.
