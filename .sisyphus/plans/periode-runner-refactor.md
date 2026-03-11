# Periode Runner Refactor Plan

## Goal

Refactor period-filter and runner orchestration to improve maintainability and runtime safety while preserving current behavior.

## Constraints

- No functional regressions for attendance submission flow.
- Keep `PERIODE_MODE` behavior (`auto`, `manual`, `legacy`).
- Keep Docker deployment behavior unchanged.

## Scope

1. Separate periode policy logic from `RunAttendance` flow.
2. Decouple runner from `config.Load` by injecting runtime settings from `main`.
3. Fix concurrency limit semantics to honor configured concurrency.
4. Keep existing logging and notification behavior.
5. Expand unit tests for period policy and helper behavior.

## Execution Phases

### Phase 1 - Boundary refactor

- Introduce a focused period-policy helper in runner package (separate file).
- Keep policy functions pure and deterministic.

### Phase 2 - Runner simplification

- Remove `config.Load()` call from `RunAttendance`.
- Extend `Runner` struct with required runtime values injected in `main`.
- Keep login and notification flow identical.

### Phase 3 - Safety correction

- Replace forced floor `max(5, r.Conc)` with clamp `>=1` so config is respected.

### Phase 4 - Verification and tests

- Update/add unit tests for period mode behavior and edge cases (year boundary, manual list normalization, legacy fallback).
- Run `go test ./...` and `go build ./...`.
- Validate compose config to ensure deployment files remain valid.

## Success Criteria

- `RunAttendance` contains orchestration only; policy helpers are extracted.
- No runtime config loading inside runner.
- Concurrency honors configured value with safe minimum of 1.
- Tests pass and build passes.
