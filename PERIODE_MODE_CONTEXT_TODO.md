# Periode Mode Context and TODO Plan

## Session Context (What We Found)

### Current behavior in code

- `CURRENT_PERIODE` is read from env config in `internal/config/config.go`.
- Runner currently does strict filtering with exact match:
  - `if c.Periode != currentPeriode { ... continue }` in `internal/runner/runner.go`.
- `Periode` is parsed from course name with regex `-(\d{4})-` in `internal/moodle/parser.go`.
- If all courses are filtered out, runner logs `no attendance found` and returns `nil` (silent success path).

### Runtime implication

- With wrong or empty `CURRENT_PERIODE`, the job can run but process zero attendance.
- This means missing attendance can happen without a hard failure signal.

### Real-world anomaly from discussion

- Example: period `0326` (March 2026) can appear in February.
- Strict equality with `CURRENT_PERIODE=0226` incorrectly skips valid upcoming attendance.

## Agreed Direction

Adopt **Periode Mode** so monthly manual env edits are not required.

### Mode design

- `PERIODE_MODE=auto` (default)
  - Automatically allow: current month `MMYY` and next month `MMYY`.
  - Example on Feb 2026: allow `0226` and `0326`.
- `PERIODE_MODE=manual`
  - Operator controls allowed values with `ALLOWED_PERIODES` (comma separated), e.g. `0226,0326`.
- Legacy fallback
  - Keep `CURRENT_PERIODE` for backward compatibility during migration, then deprecate.

## Proposed Env Model

Add these fields in config:

- `PERIODE_MODE` (`auto` or `manual`, default `auto`)
- `ALLOWED_PERIODES` (manual mode list)
- `MAX_COURSES_PER_RUN` (safety cap)

Keep existing for transition:

- `CURRENT_PERIODE` (legacy fallback only)

## TODO Checklist (Based on This Session)

## Phase 1 - Core period logic

- [ ] Add period resolver function to compute allowed periods based on mode.
- [ ] Implement `auto` mode: `{currentMonth, nextMonth}` in `MMYY` format.
- [ ] Implement `manual` mode from `ALLOWED_PERIODES`.
- [ ] Implement legacy fallback to `CURRENT_PERIODE` when mode/config not set.
- [ ] Replace strict equality check in runner with `isAllowedPeriode` check.

## Phase 2 - Safety and observability

- [ ] Log discovered period distribution each run (found periods and accepted periods).
- [ ] Change zero-match behavior from silent success to warning/error with context.
- [ ] Add `MAX_COURSES_PER_RUN` guardrail to avoid overly broad matches.

## Phase 3 - Tests

- [ ] Unit test: current month accepted.
- [ ] Unit test: next month accepted (anomaly case).
- [ ] Unit test: previous/far-future periods rejected in `auto` mode.
- [ ] Unit test: year boundary (`1225` -> `0126`) works.
- [ ] Unit test: manual mode respects `ALLOWED_PERIODES`.
- [ ] Unit test: legacy `CURRENT_PERIODE` fallback behavior.

## Phase 4 - Migration and docs

- [ ] Document new env vars and examples in README.
- [ ] Mark `CURRENT_PERIODE` as deprecated.
- [ ] Add rollout note for production (`auto` mode recommended default).

## Acceptance Criteria

- [ ] Bot processes attendance when next-period courses appear early (for example, `0326` in February).
- [ ] No monthly manual change required in `auto` mode.
- [ ] Period filtering behavior is visible in logs.
- [ ] Misconfiguration does not silently pass without operator signal.

## Suggested Implementation Order

1. Config fields and resolver
2. Runner filter replacement
3. Logging and safety guards
4. Unit tests
5. README and migration notes
