# Historical archive boundary

**Confidence label:** Historical claim

This repository intentionally preserves historical research records while separating them from current runtime authority.

## Domain split

### A) Current implementation docs (authoritative)

Use these for present behavior:
- `/home/runner/work/glass-proxy/glass-proxy/README.md`
- `/home/runner/work/glass-proxy/glass-proxy/docs/current-repository-contract.md`
- `/home/runner/work/glass-proxy/glass-proxy/docs/current-architecture.md`
- `/home/runner/work/glass-proxy/glass-proxy/docs/provider-lanes.md`
- `/home/runner/work/glass-proxy/glass-proxy/docs/session-glass-architecture.md`

### B) Historical research archive (non-authoritative by default)

Use these for chronology, incidents, and prior hypotheses:
- `/home/runner/work/glass-proxy/glass-proxy/docs/complete-chronology.md`
- `/home/runner/work/glass-proxy/glass-proxy/docs/research-chronology.md`
- `/home/runner/work/glass-proxy/glass-proxy/docs/problem-register.md`
- `/home/runner/work/glass-proxy/glass-proxy/docs/solution-register.md`
- `/home/runner/work/glass-proxy/glass-proxy/docs/source-catalogue.md`
- `/home/runner/work/glass-proxy/glass-proxy/research/bug-reports/*`

## Interpretation rule

Historical docs can describe:
- reverted behavior,
- stale assumptions,
- missing artifacts,
- environment-specific outcomes.

They are preserved for provenance, but cannot be treated as current runtime truth unless reconciled against in-tree source.

## Missing/withheld artifacts

Some historical references are not present in this publication.

See `/home/runner/work/glass-proxy/glass-proxy/docs/missing-withheld-artifacts.md`.
