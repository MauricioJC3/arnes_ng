---
name: testing-strategy
description: Choosing the right kind of test — the unit / integration / end-to-end pyramid, what each layer proves and how many to write, test doubles (stub, fake, spy, mock), isolating I/O, injecting time and randomness for determinism, what not to test, and flaky-test hygiene. Use when deciding how to cover a change or reviewing a test suite's shape. Pairs with tdd-workflow (the cycle); this is the map.
---

# Testing Strategy

`tdd-workflow` tells you how to write one test (red → green → refactor). This
skill tells you **which test to write**. A suite fails when the shape is wrong:
end-to-end tests standing in for unit tests (slow, flaky, vague failures) or unit
tests mocking so much they only test the mocks.

## The pyramid

```
        ╱  E2E  ╲          few   — whole system, real transport, critical paths only
      ╱ integ.    ╲       some   — one seam: code + a real adapter (DB, HTTP, queue)
    ╱    unit        ╲    many   — pure logic, fakes at the ports, no I/O
```

| Layer | Proves | Speed | Runs against |
|-------|--------|-------|--------------|
| **Unit** | a rule/branch/calculation is correct | microseconds | fakes at every port |
| **Integration** | your code and *one* real dependency agree | 10s–100s ms | real DB / real HTTP server / testcontainer |
| **E2E** | a full user-visible flow works wired together | seconds | the deployed surface (real HTTP + real DB) |

Rule of thumb: if a test can be a unit test, it must be. Drop to integration only
for the seam you can't fake honestly (SQL, serialization, a real HTTP client).
Reserve E2E for a handful of money paths (login, checkout, publish).

## Unit tests

- Test **observable behaviour**, not internals: return value, state change, or a
  call to a port. Not "called `helper()` twice".
- No real I/O: no network, no disk, no `time.Now()`, no `rand`, no global clock.
- Substitute at the **port** (the interface the use case owns), with a fake — not
  by monkey-patching internals.
- Table-driven for a rule with many cases; one named test per distinct behaviour.

## Integration / "back" tests

- Pick **one** seam per test: the repository against a real Postgres, the HTTP
  client against a stub server, the handler against a real router. Not "all of
  the above".
- Use a real dependency in a container or an in-memory equivalent that shares the
  same contract (e.g. real SQLite/Postgres, not a hand-rolled fake DB).
- Reset state between tests (transaction rollback, truncate, fresh container).
  Order-dependent integration tests rot fast.
- These catch what units can't: SQL typos, migration drift, JSON tag mistakes,
  timeout/retry behaviour.

## End-to-end tests

- Drive the system the way a client does: real HTTP request in, assert the
  response and the persisted side effect.
- Keep them few and independent — each seeds its own data and cleans up.
- When one fails, it tells you *something* broke, not *what*. That's why they sit
  on top of a broad unit base, never replace it.

## Test doubles — pick the weakest that works

| Double | Is | Use when |
|--------|----|----|
| **Dummy** | filler, never used | a param you must pass but the path ignores |
| **Stub** | returns canned answers | you need the collaborator to *provide* input |
| **Fake** | working lightweight impl (in-memory repo) | you need real-ish behaviour across many calls |
| **Spy** | records calls for later assertion | you must verify an interaction happened |
| **Mock** | pre-programmed with expectations, self-verifying | the interaction *is* the behaviour under test |

Prefer **fakes and stubs** over mocks. Mock-heavy tests couple to call structure
and break on every refactor. If you're asserting the order and count of five
mock calls, you're testing the implementation, not the behaviour.

## Determinism

- **Time**: inject a `Clock` (`Now() time.Time`); never call the real clock in
  code under test.
- **Randomness / UUIDs**: inject the generator, or accept a seed.
- **Concurrency**: run tests with the race detector; make the test wait on a
  condition, never a `sleep`.
- **Ordering**: no test may depend on another test having run first.

## What not to test

- Third-party framework code, the ORM, the standard library.
- Trivial getters/setters and pure pass-through wiring.
- Private helpers directly — exercise them through the public behaviour.
- Exact log strings, or generated code.

## Flaky-test hygiene

- A test that fails intermittently is a bug report — quarantine it, don't retry
  it in CI until green.
- Common causes: real sleeps, shared mutable state, wall-clock assertions,
  network calls, relying on map iteration order.

## Anti-patterns

- E2E tests used to cover logic that a unit test could pin down.
- Mocking the thing you're testing, or mocking so deep the test asserts only
  mock behaviour.
- Integration tests with no cleanup, passing only in a fixed order.
- `sleep`-based synchronization.
- One test asserting ten unrelated things.
- Chasing 100% coverage by testing getters while a branch in the pricing rule has
  none.
- No layer separation: every test boots the whole app.
