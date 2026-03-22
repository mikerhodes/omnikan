# omnikan

A read/write Kanban board that surfaces OmniFocus tasks by tag in a browser UI.
Built with Go + JXA (JavaScript for Automation via `osascript`). macOS only.

## Running

```
go run .
```

Serves on `localhost:8080` by default. Reads OmniFocus at startup; refreshes every 10 minutes in the background.

Flags:
- `-project` — OmniFocus project name to filter tasks (default: see `omnifocus.ProjectName`)
- `-addr` — listen address (default: `localhost:8080`)

## Architecture

```
main.go                      — HTTP server, in-memory cache, API handlers
internal/omnifocus/
  omnifocus.go               — Go API: TasksForTag, SwapTag, MarkComplete, MarkIncomplete
  jxa.go                     — executeScript: pipes JXA to osascript via stdin, passes args via OSA_ARGS env var
  jxa/oftasksfortag.js       — fetch tasks for a tag (uses evaluateJavascript()+OmniJS tagsMatching(), ~0.15s)
  jxa/ofswaptag.js           — swap one kanban tag for another
  jxa/ofmarktaskcomplete.js  — mark a task complete
  jxa/ofmarktaskincomplete.js — mark a task incomplete (undo)
  jxa/ofaddtask.js           — add a new task to a project with a tag
  jxa/ofdeletetask.js        — delete a task
  jxa/ofprojectid.js         — resolve a project name to its OmniFocus ID
assets/board.html            — single-page UI
assets/ember.css             — Ember design system base styles
assets/umber-light.css       — light theme
assets/umber-dark.css        — dark theme
```

## Columns / Tags

Three columns map directly to OmniFocus tags:

| Column      | Tag          |
|-------------|--------------|
| Backlog     | `backlog`    |
| Ready       | `ready`      |
| In Progress  | `inprogress` |

Moving a card calls `SwapTag` to remove the old tag and add the new one.

## Caching

- `s.board` — the `boardResponse` served to the browser
- `s.tasks` — `map[string]cachedTask` keyed by OmniFocus task ID; stores task + current column
- `moveMu` serialises all JXA calls (read + call + write held together) to prevent same-card race conditions
- Moves, completions, and undos update `s.board` and `s.tasks` immediately — no stale state on page reload
- `GET /api/board?force=true` triggers a full OmniFocus re-fetch before responding

## API

| Method | Path             | Body / Query          | Effect                            |
|--------|------------------|-----------------------|-----------------------------------|
| GET    | `/`              |                       | Serve `board.html`                      |
| GET    | `/assets/`       |                       | Serve static assets                     |
| GET    | `/api/board`     | `?force=true` optional| Return cached board as JSON             |
| POST   | `/api/move`      | `{id, newCol}`        | Swap kanban tag, update cache           |
| POST   | `/api/delete`    | `{id}`                | Delete task from OmniFocus, remove from cache |
| POST   | `/api/complete`  | `{id}`                | Mark complete, remove from cache        |
| POST   | `/api/incomplete`| `{id}`                | Mark incomplete, restore to cache       |
| POST   | `/api/add`       | `{name, col}`         | Add new task to OmniFocus, add to cache |

## UI constraints

The page header must stay compact (single line, baseline-aligned). The Ember design
system's default header is taller — don't adopt that here. The header contains three
grid cells: app title (left), keyboard shortcut hint (centre), status bar (right).

## UI behaviour

- Cards move in the DOM immediately (optimistic update); API call fires async
- Completed cards show struck-through for 60 seconds, then are removed; unchecking within that window calls `/api/incomplete`
- `pendingMoves` counter drives "Saving…" / "Last updated: HH:MM:SS" status in the header
- Notes are shown only on In Progress cards; URLs in notes are linkified

## JXA notes

See `jxa-notes.md` for detailed scripting reference including performance benchmarks,
tag manipulation patterns, and OmniFocus property quirks (all properties need `()`).
