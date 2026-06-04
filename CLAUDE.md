# omnikan

A read/write Kanban board that surfaces OmniFocus tasks by tag in a browser UI.
Built with Go + JXA (JavaScript for Automation via `osascript`). macOS only.

## Running

```
go run .
```

Flags:
- `-project` — OmniFocus project name to filter tasks (required)
- `-addr` — listen address (default: `localhost:8080`)
- `-dynamic` — serve assets from disk instead of embedded binary (useful for UI development)

## UI behaviour

- Cards move in the DOM immediately (optimistic update); API call fires async
- Completed cards show struck-through for 60 seconds, then are removed; unchecking within that window calls `/api/incomplete`
- `pendingMoves` counter drives "Saving…" / "Last updated: HH:MM:SS" status in the header
- Notes are shown only on In Progress cards; URLs in notes are linkified

## Workflow: adding new OmniFocus JXA scripts

1. Write a draft `.js` script and run it directly with `osascript` to test. Create dummy tasks in OmniFocus as needed (the existing `ofaddtask.js` script helps with this).
2. Iterate on the script until the output is correct.
3. Only then write the Go wrapper and any UI changes.

## JXA notes

See `jxa-notes.md` for detailed scripting reference including performance benchmarks,
tag manipulation patterns, and OmniFocus property quirks (all properties need `()`).
