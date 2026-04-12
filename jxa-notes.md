# JXA Notes: Scripting OmniFocus

Notes from building omnikan and github-to-omnifocus. JXA
(JavaScript for Automation) is Apple's scripting bridge available
since OS X Yosemite.

Scripts have two layers:

- **JXA** — the outer `osascript` shell. Reads `OSA_ARGS`, calls `Application("OmniFocus").evaluateJavascript()`. Keep this layer minimal.
- **OmniJS** — runs inside OmniFocus. Does the actual work. Faster (no per-property bridge crossings), better documented at https://omni-automation.com/omnifocus/index.html, and humans can run and debug snippets directly in OmniFocus's automation console (Help → Automation Console).

All logic should live in the OmniJS layer. As an AI, OmniJS is harder to get right without direct execution, but it should still be the default.

---

## Suggested workflow

1. **Write a draft OmniJS function** targeting the data you need. Hard-code any values for now.
2. **Create test data** in OmniFocus if needed. Use the existing `ofaddtask.js` script to create tasks.
3. **Test the OmniJS snippet** — humans can paste it directly into the automation console for fast iteration. AIs need to run the full script via `osascript`.
4. **Run the full script** with `osascript -l JavaScript my-script.js | jq .` and inspect the output.
5. **Iterate** until the output is correct and edge cases (missing task, wrong tag, null project) are handled.
6. Only once the script is stable, write the Go wrapper and any UI changes.

---

## Script structure

Every script follows this pattern — a JXA IIFE that stringifies an OmniJS arrow function and passes args as JSON:

```js
// @ts-check
// Description of what this script does.
// Accepts { "key": "value" } via OSA_ARGS.
//
// Call it:
//   set -gx OSA_ARGS '{"key":"value"}'
//   osascript -l JavaScript myscript.js | jq .
(() => {
  "use strict";

  ObjC.import('stdlib')
  const argsJson = $.getenv('OSA_ARGS')

  const script = (jsonString) => {
    const args = JSON.parse(jsonString)
    // OmniJS code here — plain property access, no ()
    return JSON.stringify(result)
  }

  return Application("OmniFocus").evaluateJavascript(
    `(${script})(${JSON.stringify(argsJson)})`
  )
})()
```

The OmniJS `script` function is real JS — syntax highlighted, linted, no string escaping.
`Function.toString()` serialises it; the template literal wraps it in an IIFE and passes
the args JSON string as its sole argument. The function parses args itself.

For console testing, call `script('{"key":"value"}')` directly.

### Running from Go

Pass the script file via stdin to `osascript -l JavaScript`. Pass arguments via `OSA_ARGS`.
The OmniFocus scripting bridge is single-threaded — make calls sequentially from Go.

```go
func executeScript(jsCode []byte, args []byte) ([]byte, error) {
    cmd := exec.Command("/usr/bin/osascript", "-l", "JavaScript")
    cmd.Env = append(os.Environ(), "OSA_ARGS="+string(args))
    stdin, err := cmd.StdinPipe()
    if err != nil {
        return nil, err
    }
    go func() {
        defer stdin.Close()
        stdin.Write(jsCode)
    }()
    return cmd.Output()
}
```

---

## OmniJS reference

Properties are plain fields (no `()` required). IDs use `t.id.primaryKey`.

### Look up by ID

```js
const t = Task.byIdentifier(args.id)        // null if not found
const proj = Project.byIdentifier(args.id)  // null if not found
```

### Look up by name

```js
const tag  = flattenedTags.byName(args.tag)
const proj = flattenedProjects.byName(args.projectName)
```

### Filter active tasks

Use `Task.Status` rather than checking `completed`:

```js
const activeStates = [
  Task.Status.Available,
  Task.Status.DueSoon,
  Task.Status.Next,
  Task.Status.Overdue,
  Task.Status.Blocked
]
const tasks = proj.tasks.filter(t => activeStates.includes(t.taskStatus))
```

### Task properties

```js
t.id.primaryKey   // string ID
t.name            // string
t.note            // string
t.added           // timestamp string (creation time)
t.tags            // Tag[]
t.taskStatus      // Task.Status enum value
t.containingProject  // Project or null
```

### Tags

```js
t.addTag(tag)
t.removeTag(tag)
t.tags.map(tg => tg.name)
```

### Create a task

```js
const project = Project.byIdentifier(args.projectId)
const tag = flattenedTags.byName(args.tag) || new Tag(args.tag)  // create tag if absent
const task = new Task(args.name, project)
task.addTag(tag)
```

### Delete a task

```js
const task = Task.byIdentifier(args.id)
deleteObject(task)
```

### Mutate task properties

```js
t.name = args.name
t.note = args.note
```

### Complete / incomplete

```js
t.markComplete()
t.markIncomplete()
```

---

## Dates

OmniFocus stores due dates as midnight local time. `toISOString()` shifts to UTC —
format manually to preserve the local date:

```js
function toLocalDateString(d) {
  if (!d) return null
  return d.getFullYear() + "-" +
         String(d.getMonth() + 1).padStart(2, "0") + "-" +
         String(d.getDate()).padStart(2, "0")
}
```

When setting dates, construct from components — `new Date("2026-03-20")` parses as
UTC and shifts in non-UTC timezones:

```js
task.dueDate = new Date(2026, 2, 20)   // ✅ local midnight, month is 0-indexed
task.dueDate = new Date("2026-03-20")  // ❌ UTC, shifts in non-UTC timezones
task.dueDate = null                    // clear
```

---

## Quick reference

| Goal | OmniJS |
|------|--------|
| Find task by ID | `Task.byIdentifier("abc")` — null if not found |
| Find project by ID | `Project.byIdentifier("abc")` — null if not found |
| Find tag by name | `flattenedTags.byName("backlog")` |
| Find project by name | `flattenedProjects.byName("My Project")` |
| Project's tasks | `proj.tasks` |
| Tag's tasks | `tag.tasks` |
| Task ID | `t.id.primaryKey` |
| Task tags | `t.tags.map(tg => tg.name)` |
| Task's project | `t.containingProject` (null for inbox tasks) |
| Add tag | `t.addTag(tag)` |
| Remove tag | `t.removeTag(tag)` |
| Complete | `t.markComplete()` |
| Undo completion | `t.markIncomplete()` |
| Set due date | `t.dueDate = new Date(2026, 2, 20)` (month 0-indexed) |
| Clear due date | `t.dueDate = null` |
| Flag | `t.flagged = true` |
| Create task in project | `new Task(name, project)` |
| Create tag if absent | `flattenedTags.byName(name) \|\| new Tag(name)` |
| Delete task | `deleteObject(task)` |
| Set task name | `t.name = "new name"` |
| Set task note | `t.note = "new note"` |
