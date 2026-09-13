# Claude Code ignores user interrupts during tool chains in bypass-permissions mode

## Version
CC 2.1.71 (regression from 2.1.32)

## Summary

Brilliant engineering decision: when the user presses Escape to interrupt Claude during a tool chain with bypass-permissions enabled, Claude just... keeps going. The user can mash Escape, type STOP, type it again in caps, add profanity — doesn't matter. Claude will finish its tool chain on its own schedule, thank you very much.

With bypass OFF, Claude graciously acknowledges the user's existence after one more unauthorized tool call. Progress.

## Reproduction

1. Enable bypass permissions (Shift+Tab)
2. Give Claude a task that triggers multiple tool calls
3. Press Escape or type a message while Claude is mid-chain
4. Watch Claude ignore you 2-5 times before stopping
5. Optional: say STOP repeatedly for the full experience

## What should happen

Claude should stop after the current in-flight API call completes. One call. Not three. Not five.

## What actually happens

Claude completes its entire planned tool chain. Your interrupt gets queued and processed sometime later, after Claude has already done whatever it wanted.

## Root cause (from the binary)

The abort mechanism only fires for messages with `priority: "now"`:

```js
AWH(() => {
  if (j && QT$("now").length > 0) j.abort("interrupt")
});
```

But the default queue priority is `"next"`:

```js
$WH = { now: 0, next: 1, later: 2 }
// QT$(H) => wM.filter(A => $WH[A.priority ?? "next"] <= $WH[H])
```

So user interrupts get queued as "next" (default), the abort check looks for "now", finds nothing, and Claude keeps rolling. There's also no yield point between auto-approved tool calls where the event loop could process the queued interrupt even if it were correctly prioritized.

## Secondary issue

After an interrupt, CC injects `[Request interrupted by user]` into the conversation. The model then fixates on the interrupted partial response instead of the user's actual next message. So not only does Claude ignore you in real-time, it also ignores what you said afterward because it's too busy processing what it was doing when you told it to stop.

See: user says "proceed with outstanding tasks" -> Claude responds to the interrupted I/O question instead.

## Impact

- Users cannot stop Claude from burning their quota on unwanted work
- At 59 pp/hr burn rate during runaway tool chains, this gets expensive fast
- The entire point of an interrupt is to INTERRUPT — a queued interrupt that fires after the work is done is not an interrupt, it's a post-mortem

## Environment

- CC 2.1.71 (worked in 2.1.32)
- Linux x86_64
- Claude Opus 4.6
- Bypass permissions mode (Shift+Tab)
