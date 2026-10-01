# Documentation governance

## Scope

This policy governs repository docs so that implementation and documentation stay synchronized.

## Ownership model

- **README owner:** repository maintainers.
- **Current architecture/contract owners:** maintainers changing `/cmd` or `/internal` runtime behavior.
- **Historical archive owners:** maintainers curating chronology and provenance docs.
- **Security doc owner:** maintainers changing runtime exposure or credential/data handling boundaries.

## Required labels for substantive sections

Use one of:
- **Verified in source**
- **Historical claim**
- **External reference**

These labels prevent historical narrative from being read as current runtime truth.

## PR documentation gate (required)

Any PR that changes runtime behavior must:
1. Update affected current docs in same PR, or
2. Explicitly state why no doc change is needed.

## Source-to-doc mapping rule

Technical claims in current docs must map to concrete paths in `/cmd` or `/internal`.

## Link and policy checks

CI runs repository doc checks and fails on:
- broken local markdown links,
- missing required confidence labels in governance-scoped docs.

## Change policy

- No dead local links in `README.md` or `/docs/*.md`.
- Missing historical artifacts must be listed in `missing-withheld-artifacts.md`.
- Historical docs can be expanded, but must not silently override current-source docs.
