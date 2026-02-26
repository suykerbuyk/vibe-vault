---
date: 2026-02-11
type: session
project: my-cli-tool
branch: fix/timezone-flake
domain: personal
model: claude-sonnet-4-5-20250929
session_id: "b2c3d4e5-f6a7-8901-bcde-f23456789012"
iteration: 1
duration_minutes: 32
messages: 18
tokens_in: 38000
tokens_out: 12000
status: completed
tags: [cortana-session, debugging]
summary: "Debug flaky CI test caused by timezone-dependent date formatting"
previous: "[[2026-02-10-01]]"
---

# Debug flaky CI test caused by timezone-dependent date formatting

## What Happened

Investigated intermittent test failure in `format-output.test.ts` that only failed in CI. Root cause: date formatting assertion assumed `America/Chicago` timezone, but CI runs in UTC. Fixed all 7 call sites of `formatDate()` to accept explicit timezone. Added `TZ=Asia/Tokyo` CI matrix entry as a canary.

## Key Decisions

- Explicit timezone in all date formatting, not just the broken test — audited all call sites, found 3 more that would break under non-US timezones
- CI timezone matrix over mocking Date — mocking hides real bugs, running under non-local timezone catches assumptions anywhere

## What Changed

- `src/utils/format-date.ts` (modified)
- `src/utils/format-date.test.ts` (modified)
- `src/commands/report.ts` (modified)
- `.github/workflows/ci.yml` (modified)

## Open Threads

- [ ] Audit other date-related utilities for similar assumptions

---
*vv v0.1.0*
