# Serializer Analysis — Full Trace from Introduction to Present

## When Introduced
- **MITM proxy**: `BatchAffinitySerializer` existed from ~Feb 18 (INSIGHTS-FEB18-SOLUTIONS)
- **Glass-proxy**: `serializer.go` created March 14, `BatchSize=5`, `IdleTimeoutSec=15`
- **MITM proxy optimal config** (Feb 23, GOLDEN-14): `idle_timeout_sec=15`, NO batch size limit
- **DIAMOND-11** (Mar 3): MITM changed to `batch_size=8`, `idle_timeout=12s`

## Design Origin
The serializer was designed to solve **inter-session cache interleaving**:
- Anthropic's cache is prefix-matched (LRU, ~2-4 concurrent slots)
- When sessions A and B alternate, each request rebuilds the entire prefix
- Solution: batch requests per session so only the FIRST call breaks cache

Source: `RESEARCH-INTERLEAVING-FIX.md` (Feb 17-18)

## Why Per-Session Proxy Was Rejected
The MITM proxy tried per-session isolation (each CC → own proxy). It FAILED because:
1. Each proxy trimmed system prompts independently → different bytes → no shared cache
2. Watermarks diverged between proxies → earlier prefix divergence
3. Tool stripping inconsistency between proxy instances
Source: `RESEARCH-INTERLEAVING-FIX.md` §3

**This reasoning does NOT apply to glass-proxy** because glass-proxy uses canonical
system prompt caching — every session sees identical system prompt bytes.

## Database Evidence: Did the Serializer Help?

### Daily break rates (from glass_debug.db, 21K+ requests)

| Day | Reqs | CC% | Big Breaks | Break Rate | Sessions |
|-----|------|-----|------------|------------|----------|
| Mar 04 | 809 | 2.8% | 9 | 1.1% | 1 |
| Mar 05 | 1099 | 7.4% | 49 | 4.5% | 1 |
| Mar 06 | 1631 | 6.6% | 73 | 4.5% | 1 |
| Mar 07 | 1865 | 8.3% | 117 | 6.3% | 1 |
| Mar 08 | 1216 | 8.2% | 71 | 5.8% | 1 |
| Mar 09 | 2487 | 9.4% | 175 | 7.0% | 1 |
| Mar 10 | 3098 | 7.8% | 195 | 6.3% | 4 |
| Mar 11 | 2321 | 6.5% | 100 | 4.3% | 7 |
| Mar 12 | 958 | 5.2% | 24 | 2.5% | 3 |
| Mar 13 | 1812 | 10.0% | 77 | 4.2% | 5 |
| **Mar 14** | **1469** | **12.5%** | **57** | **3.9%** | **6** ← serializer introduced |
| Mar 15 | 1253 | 5.2% | 47 | 3.8% | 5 |
| Mar 16 | 1130 | 10.5% | 91 | 8.1% | 4 ← saturation flush + agent_tool regression |

### Aggregate comparison

| Period | Reqs | CC/day (M) | Hit% | Break Rate |
|--------|------|-----------|------|------------|
| Pre-serializer (Mar 4-13) | 17,296 | 12.9M | 92.3% | 5.1% |
| Post-serializer (Mar 14-16) | 3,834 | 12.7M | 91.3% | 5.1% |

**The serializer made zero measurable difference.**

### Session switch analysis (Mar 10+, multi-session days only)

| Day | Switches | Switch Breaks | Avg Switch CC | Avg Same CC |
|-----|----------|---------------|---------------|-------------|
| Mar 10 (pre) | 249 | 30 | 12,581 | 8,005 |
| Mar 11 (pre) | 440 | 49 | 10,828 | 4,927 |
| Mar 12 (pre) | 192 | 17 | 8,529 | 3,888 |
| Mar 13 (pre) | 253 | 39 | 13,829 | 7,320 |
| Mar 14 (post) | 178 | 17 | 14,680 | 7,282 |
| Mar 15 (post) | 273 | 40 | 22,566 | 3,724 |
| Mar 16 (post) | 60 | 16 | 20,449 | 14,199 |

Serializer reduced switch COUNT ~40% but COST PER SWITCH increased ~70%.
Net effect: neutral.

## Why It Doesn't Work

1. **BatchSize=5 is too small**: Forces session switch every 5 requests even when
   the session is actively working. The MITM proxy had NO batch limit — only idle timeout.

2. **Session switch = full prefix rebuild regardless**: When sessions have different
   message histories (they always do), switching means Anthropic rebuilds the full
   non-shared prefix. The serializer delays switches but can't prevent the cost.

3. **The fundamental problem**: Two sessions with different messages CANNOT share
   Anthropic cache entries beyond the shared system prompt (~25K tokens). The
   serializer batches reduce switch frequency but each switch still costs 30-200K
   cache_creation tokens.

## Why Per-Session Serializer Could Work NOW

The MITM proxy rejected per-session proxies because each proxy trimmed system prompts
differently. Glass-proxy uses **canonical system prompt caching** — every session gets
identical system prompt bytes. This means:

- Per-session serializer: each session gets its own Anthropic cache slot
- No interleaving at all — zero switch cost
- Shared system prompt prefix still works (canonical bytes)
- No BatchSize needed — each session runs uninterrupted

The MITM proxy's rejection reasoning is obsolete for glass-proxy's architecture.

## MITM Proxy Optimal Config (for reference)
- `idle_timeout_sec=15` (Feb 23, GOLDEN-14: achieved 0% break rate)
- `batch_size=8` (Mar 3, DIAMOND-11)
- No per-request batch limit in the original design
- Subagent parent serialization (Feb 25, v5)
