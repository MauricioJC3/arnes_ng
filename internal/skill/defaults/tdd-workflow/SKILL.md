---
name: tdd-workflow
description: Test-Driven Development — the red/green/refactor cycle, writing the test first, minimal implementation, refactoring under a green bar. Use when building a feature or fixing a bug test-first.
---

# TDD Workflow

Write the test first. The test is the specification: if you can't write it, you
don't yet understand the requirement.

## The cycle

```
RED      → write one failing test for the next small behavior
GREEN    → write the minimum code to make it pass
REFACTOR → improve the code with every test still green
repeat
```

Never skip RED. Watching the test fail proves the test actually exercises the
code — a test that passes before you write the implementation tests nothing.

## RED — the failing test

- Test **behavior**, not implementation: "returns 0 for an empty cart", not
  "calls `sum()` once".
- Name the case by scenario: `TestTransfer_InsufficientFunds_Rejected`.
- One behavior per test. Multiple assertions are fine when they describe the same
  outcome.
- Cover the happy path first, then error cases, then edge cases.
- Run it. Confirm it fails for the **expected reason** (assertion), not a compile
  error or a typo.

## GREEN — minimum to pass

- Write the simplest thing that makes the bar green, even if it's obviously
  incomplete. The next test forces the next piece of real logic.
- No speculative generality, no optimization, no handling cases you don't have a
  test for yet (YAGNI).
- If making the test pass is hard, the test is probably too big — split it.

## REFACTOR — clean under green

- Remove duplication, clarify names, simplify structure.
- Change one thing at a time; keep the suite green throughout.
- Refactor both the production code **and the tests** — test rot is real.
- Commit at a green bar, ideally after each refactor.

## Arrange–Act–Assert

Every test has three parts, in this order:

```
Arrange  set up inputs and dependencies (fakes at the ports)
Act      call the one thing under test
Assert   check the outcome — return value, state change, or interaction
```

## Bug fixes are TDD too

1. Write a test that reproduces the bug. It fails (RED).
2. Fix the code until it passes (GREEN).
3. The test stays as a regression guard.

## When TDD earns its keep

| Situation | Value |
|-----------|-------|
| New logic with clear rules | High |
| Bug fix | High — reproduce first |
| Complex branching / calculations | High |
| Exploratory spike | Low — spike, throw away, then TDD the real thing |
| Pure layout / wiring | Low |

## Anti-patterns

- Writing the implementation first and backfilling tests.
- Skipping RED — never seeing the test fail.
- Testing private helpers instead of observable behavior.
- One giant test with ten unrelated assertions.
- Leaving tests broken "temporarily" during a refactor.
