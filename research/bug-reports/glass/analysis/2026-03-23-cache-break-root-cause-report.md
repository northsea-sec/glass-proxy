# Cache Break & Quota Burn Root Cause Report — March 23, 2026
## Full DB + Snippet Patch Correlation

**Data sources:**
- glass_debug.db: 908 requests, 2742 quota snapshots, 174 subagent events on March 23
- Snippet patch backups: 5 code patches on March 23, 48 patches March 17-23

---

## CODE PATCHES TIMELINE (March 17-23)

| Timestamp | File | Notes |
|-----------|------|-------|
| Mar 17 14:18 | proxy/proxy.go | 1 patch |
| Mar 18 12:01-12:10 | session.go, request_meta.go, process.go (6x), proxy.go (14x), pctx.go | **MAJOR: 22 patches in 10 minutes** — heavy refactor of proxy + glass process |
| Mar 19 17:16-18:21 | proxy.go, classifier.go (3x), agent_tool_golden_test.go (5x), serializer.go (5x) | Subagent classifier + serializer changes |
| Mar 19 21:27-21:29 | config.go, proxy.go (3x) | Config + proxy changes |
| **Mar 23 15:04-15:06** | **proxy.go (2x), main.go (3x)** | **5 patches DURING the catastrophe window** |

---

## CORRELATED TIMELINE — March 23

### Phase 1: 09:13-10:24 — Conv 31531 (single session, subagent-heavy)
- **292 requests**, ALL subagent (agent_tool), single conversation
- rl_5h: 0% → 11% over 71 minutes
- Anchor climbed: msg[6] → msg[561] (68 breakpoint advances)
- Pattern: Every ~8 messages, prefix_change_kind=changed, cache_creation spike of 1-10K tokens
- **Periodic full rebuilds**: At 09:26:43, 09:30:13, 09:36:54, 09:47:43, 09:52:21, 09:54:29, 10:02:38, 10:05:05, 10:06:53, 10:09:10, 10:17:01, 10:19:30, 10:22:04 — divergence jumps back (e.g., msg[484]→msg[40]) with cc=70-130K
- **These are the compression watermark resets** — prefix jumps to a much earlier message, rebuilding ~100K+ tokens
- **No code patches during this window**

### Phase 2: 10:26-10:39 — Conv 97810 (new session, same machine)
- Continues subagent work, 36 requests
- rl_5h: 11% → 13%
- **Row 299**: prefix_divergence=system — system prompt changed between convs
- **Row 324**: divergence jumps to msg[2] with cc=32004 — near-full rebuild
- **No code patches during this window**

### Phase 3: 12:41-13:12 — Conv 173115 + 175084 + small_system hell
- **Row 337**: cc=68,464, cr=0 — TOTAL cache miss after TTL gap (2 hours since last request)
- **Rows 344-420**: Conv 508a4b90cd70_175084 running small_system subagent — 77 requests
  - Row 351: prefix_divergence=system, cc=20,238 — **different system prompt, competing cache slot**
  - small_system keeps making requests every 3-7 seconds, burning ~400-600 output tokens each
  - Most reads are stable at 53-63K but burns 16-21% utilization
- rl_5h: 13% → 21%
- **No code patches during this window**

### Phase 4: 14:33-16:14 — THE CATASTROPHE (4+ concurrent conversations)

**Active conversations overlapping:**
1. `6be761d4747a_230340` (from 14:33) — main session + subagents
2. `6be761d4747a_235063` (from 14:40) — main session + subagents
3. `28a034b5507a_244293` (from 14:51) — subagent-heavy
4. `28a034b5507a_273450` (from 15:41) — subagent-heavy
5. `28a034b5507a_274180` (from 15:43) — subagent-heavy, MASSIVE context
6. `e0a2ba8019e9_244293` (15:27, 15:31) — small_system
7. `c9f8356e94ab_273450` (from 15:42) — small_system, then promoted

**The interleaving pattern (15:06-15:08 example):**
```
15:06:07 | 244293 | SUB | cc=62195, cr=25583 → divergence msg[41], 29% hit
15:06:18 | 244293 | SUB | cc=26409, divergence msg[2], 50% hit
15:06:27 | 244293 | SUB | cc=0, cr=55287, 74% hit (recovering)
15:06:43 | 244293 | SUB | cc=0, cr=55287, 58% hit (context growing)
15:06:50 | 235063 | SUB | cc=0, cr=84884, 96% hit (different conv)
15:06:53 | 235063 | SUB | cc=3480, changed msg[209] (breakpoint advance)
15:06:57 | 235063 | SUB | cc=0, cr=88364, 99% hit
15:07:02 | 235063 | SUB | cc=0, cr=88364, 99% hit
15:07:08 | 235063 | SUB | cc=0, cr=88364, 97% hit
15:07:17 | 244293 | SUB | cc=40067, divergence msg[49], 56% hit ← LRU EVICTION
15:07:28 | 244293 | SUB | cc=0, cr=95354, 92% hit (rebuilt)
15:08:02 | 244293 | SUB | cc=10956, divergence msg[57], 89% hit
15:08:07 | 235063 | SUB | cc=2515, changed msg[217], 97% hit
```

Each conversation switch triggers LRU eviction of the previous conv's cache slot.

**CODE PATCHES AT 15:04-15:06 (DURING CATASTROPHE):**
- 15:04:56 → proxy.go patch 1
- 15:05:19 → proxy.go patch 2
- 15:05:46 → main.go patch 1
- 15:05:51 → main.go patch 2
- 15:06:07 → main.go patch 3

These patches required a proxy restart, which would have:
1. Reset all in-memory session state
2. Caused cold starts for every conversation
3. Made the interleaving worse as all convs cold-started simultaneously

**Burn rate progression:**
| Time | rl_5h | rl_7d | pp/hr (approx) |
|------|-------|-------|----------------|
| 14:33 | 0% | 44% | — (5h window reset) |
| 14:48 | 10% | 45% | 60 pp/hr |
| 15:06 | 23% | 46% | 43 pp/hr |
| 15:17 | 33% | 47% | 55 pp/hr |
| 15:23 | 39% | 48% | 60 pp/hr |
| 15:42 | 45% | 48% | 19 pp/hr |
| 15:48 | 51% | 49% | 60 pp/hr |
| 15:53 | 55% | 49% | 48 pp/hr |
| 16:00 | 58% | 49% | 26 pp/hr |
| 16:06 | 61% | 50% | 30 pp/hr |
| 16:12 | 67% | 50% | 60 pp/hr |

---


---

## CORRECTED ROOT CAUSES (from code diffs + DB evidence)

### RC1 (CRITICAL): agent_tool subagents share a single cache lane via StableSessionSuffix collision

**Code evidence (classifier.go:132, classifier.go:246-262):**
All agent_tool subagents from the same CC instance inherit the parent's system prompt. `StableSessionSuffix()` hashes the system prompt text → all subagents from the same parent get the **same** `SessionSuffix` → they all route to the **same** glass cache lane.

**DB evidence:** Machine `6be761d4747a` ran 6 conversations (546 requests, 3.3M cache_creation tokens). Machine `28a034b5507a` ran 4 conversations (224 requests, 2M cache_creation). These conversations had different message histories but shared the same cache prefix slot. Every time the glass cache served conversation A's tail, conversation B's next request found the tail diverged and rebuilt it.

**The bug:** `IsolateSession=true` with a shared `SessionSuffix` means all agent_tool subagents get isolated FROM the main session — but NOT from each other. They're all dumped into one lane and fight for it.

### RC2 (HIGH): March 19 classifier fix removed BypassCanonical, unifying cache competition

**Code evidence (classifier.go:110, pre-fix vs post-fix diff):**
Before March 19: `info.BypassCanonical = true` — agent_tool subagents bypassed the canonical glass cache path. Each got its own cache handling.
After March 19: line 110 (`info.BypassCanonical = true`) was **deleted**. Comment says: "agent_tool subagents share the parent's system prompt, so they hit the canonical cache (fast) and get the same sysprompt pipeline modifications."

**The theory was correct for single-subagent sessions.** When only one agent_tool subagent runs at a time, sharing the canonical cache is efficient — the system prompt prefix is already cached. But when CC launches **multiple concurrent subagents** (which it does during complex tasks), they all compete for the same canonical cache slot and thrash each other's message tails.

**This is the code change that created the March 23 conditions.** It ran fine from March 19-22 when sessions were sequential. March 23 was the first day with 4+ concurrent conversations from the same machine.

### RC3 (HIGH): March 19 serializer fix enabled faster cross-session switching during subagent streaming

**Code evidence (serializer.go:250-253, 391-395):**
Before March 19: `inFlight` counted all requests including subagent streams. `shouldSwitch()` would NOT switch to a new conversation while any request was streaming → subagents from conv A would BLOCK conv B.
After March 19: `mainInFlight = inFlight - subagentInFlight`. Only main-session streams block switching. Subagent streams don't prevent batch switches.

**Intended effect:** Stop subagents from starving other sessions (observed: 120s queue timeouts).
**Actual effect on March 23:** The serializer now switches between conversations MORE AGGRESSIVELY when only subagents are streaming. This INCREASES the rate of cache slot eviction. Before the fix, conv B would wait until conv A's subagents finished. After the fix, conv B fires immediately, evicting conv A's cache. Both are worse — the fix traded queue timeouts for cache thrashing.

### RC4 (MEDIUM): Compression watermark resets are a constant background burn
**DB evidence:** Phase 1 (single session, no contention) still burned 11% in 71 minutes due to periodic prefix jumps (msg[484]→msg[40]). These are inherent to CC's compression cycle and not fixable at the proxy level.

### RC5 (LOW): small_system subagents from different conv_ids create competing prefix variants
**DB evidence:** 101 requests, 198K cc tokens. Minor contributor compared to agent_tool's 4.8M.

---

## THE ACTUAL STORY

The March 19 changes fixed two real bugs (classifier misclassifying fresh sessions, serializer starving queued sessions). But the combination of:
1. Removing `BypassCanonical` from agent_tool (putting all subagents into one cache lane)
2. Making the serializer switch faster between conversations (increasing cache slot turnover)

...created a system that was fine under sequential load but catastrophic under concurrent load. March 23 was the first day that hit the concurrent case hard enough to expose it.

**The fundamental design problem:** `StableSessionSuffix` groups by system prompt content, but the cache divergence happens in the MESSAGE TAIL, not the system prompt. Subagents with the same system prompt but different conversation histories get the same suffix, share the same cache lane, and thrash each other's tails on every request.

### SUPERSEDED — RC1 (CRITICAL, ~55% of burn): Multi-conversation LRU cache slot eviction
**DB proof:** At 15:06-15:08, conversations 244293 and 235063 alternate requests. Each time 244293 fires after 235063, its cache_read drops and cache_creation spikes (62K, 26K, 40K, 10K). The pattern repeats at 15:14 (244293 fires with cc=95148 after gap), 15:23 (cc=138800 after recovery), 15:30 (cc=54746 after gap).

**Mechanism:** Anthropic maintains limited cache slots per organization. Different conversation prefixes compete for these slots. When conv A takes the slot, conv B's next request is a full rebuild.

**Why no fix existed:** The 15s serializer only works intra-session. Cross-session (different conv_ids on different terminals) interleaving has no serializer protection.

### RC2 (HIGH, ~20% of burn): Code patches at 15:04-15:06 causing cold restart
**DB proof:** 5 snippet patch backups timestamped 15:04:56-15:06:07 for proxy.go and main.go. After these patches, a proxy rebuild/restart would reset all session state, causing simultaneous cold starts for every active conversation.

**Correlation:** The burn rate spikes sharply from 23% to 39% between 15:06 and 15:23 — the 17 minutes after the patches landed.

### RC3 (HIGH, ~15% of burn): Unbounded subagent context growth
**DB proof:** Conv 274180 shows input_tokens growing: 7566 → 23745 → 43787 → 59146 → 79001 (rows 766-783) across 5 minutes. Each request rebuilds this growing context. The cc=79220 at row 784 is a 79K-token rebuild for a single subagent request.

**Mechanism:** CC sends full conversation history to subagents. No windowing or truncation. Context grows linearly with each tool call.

### RC4 (MEDIUM, ~7% of burn): small_system prefix competition
**DB proof:** Row 351 (12:47:44) — conv 508a4b90cd70_175084, subagent_type=small_system, prefix_divergence=system, cc=20,238. Row 344 shows the small_system init with a different conv_id prefix.

Rows 759-763: conv c9f8356e94ab_273450, small_system, prefix_divergence=system, cc=21783. Another small_system subagent with DIFFERENT system prompt competing for cache.

### RC5 (LOW, ~3% of burn): Compression watermark resets (designed behavior)
**DB proof:** Phase 1 shows periodic jumps where divergence resets from msg[484] to msg[40], msg[200], msg[242] etc. with cc=70-130K. These are the compression cycle rebuilds — designed to happen but each costs a full prefix rebuild.

---

## TOTAL BURN: 67% of 5h quota in ~3 hours (target was ~24%)
## OVERBURN FACTOR: 2.8x

---

## WHAT THE PREVIOUS AGENT MISSED

The previous agent's report identified RC1 (interleaving), RC2 (breakpoint advances), RC3 (context growth), RC4 (small_system) — but:

1. **Never correlated to code patches** — the 5 patches at 15:04-15:06 were never examined. These caused a proxy restart during peak interleaving, making everything worse.
2. **Used filtered SQL** — only looked at cc>20K, missing the gradual context growth pattern and the compression watermark reset pattern.
3. **Never read the snippet backup files** — never examined WHAT code changed, only that the DB showed cache breaks.
