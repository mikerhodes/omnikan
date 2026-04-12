# JXA Notes: Scripting OmniFocus

Notes from building omnikan and github-to-omnifocus. JXA
(JavaScript for Automation) is Apple's scripting bridge available
since OS X Yosemite.

There are two scripting contexts in play here:

- **OmniJS** — runs inside OmniFocus via `app.evaluateJavascript()`. Preferred: it's
  faster, has better documentation at https://omni-automation.com/omnifocus/index.html,
  and humans can run and debug snippets directly in OmniFocus's built-in automation console.
- **JXA** — the outer osascript shell that calls OmniFocus via the macOS scripting bridge.
  Documentation is sparse and inconsistent. Use JXA only for the boilerplate that connects
  to OmniFocus and passes args; push as much logic as possible into OmniJS.

As an AI, OmniJS is harder to get right (no direct execution, harder to iterate), but it
should still be the default choice. Use pure JXA only when OmniJS can't do the job.

---

## Suggested workflow

JXA scripting is iterative — the bridge is opaque and documentation is sparse, so
testing in the shell is much faster than round-tripping through application code.

1. **Write a draft OmniJS script** targeting the data you need. Hard-code any IDs or names for now.
2. **Create test data** in OmniFocus if needed (inbox tasks, tagged tasks, etc.). For scripted apps, use the equivalent `add` script if one exists.
3. **Test the OmniJS snippet** — humans can paste it directly into OmniFocus's automation console (Help → Automation Console) for fast iteration. AIs need to run the full script via `osascript`.
4. **Run the full script** with `osascript -l JavaScript my-script.js | jq .` and inspect the output.
5. **Iterate** until the output is correct and edge cases (missing task, wrong tag, null project) are handled.
6. **Switch to `OSA_ARGS`** for any values that will be dynamic, and verify the script still works when args are passed via the environment variable.
7. Only once the script is stable, write the application code (Go wrapper, API handler, UI).

This order matters: bugs are much easier to diagnose in isolation than inside an application call stack.

---

## Running scripts

```bash
# Inline
osascript -l JavaScript -e 'var doc = Application("OmniFocus").defaultDocument; doc.inboxTasks().length;'

# From file
osascript -l JavaScript /path/to/script.js

# With jq
osascript -l JavaScript my-script.js | jq '.[].name'
```

Always end scripts with a value — the last evaluated expression is
printed to stdout. Use `JSON.stringify(result)` for structured data.

### Running JXA from Go

Pass the script via stdin to `osascript -l JavaScript`. Pass arguments
via the `OSA_ARGS` environment variable as a JSON string.

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

Reading args in the script:

```js
var args = JSON.parse($.getenv('OSA_ARGS'))
// args.tag, args.projectName, etc.
```

The OmniFocus scripting bridge is single-threaded. Concurrent
`osascript` invocations targeting OmniFocus will contend or deadlock.
Make calls sequentially from Go.

---

## JXA context

### Boilerplate

```js
ObjC.import('stdlib')
var app = Application("OmniFocus")
var doc = app.defaultDocument
```

`defaultDocument` is the main OmniFocus database. All queries start here.

OmniFocus must be running. JXA cannot launch it headlessly — it needs a UI session.

### Properties require `()`

OmniFocus objects expose properties as zero-argument functions, not plain properties:

```js
task.name()        // "Buy milk"
task.id()          // "iAKv1Uo8XqW"
task.completed()   // false
task.flagged()     // true
task.note()        // "some details"
task.dueDate()     // JS Date object, or null
task.tags()        // [Tag, Tag, ...]
task.containingProject()  // Project, or null
```

Writing `task.name` gives you a specifier object, not a string.

### `whose()` — server-side filtering

`whose()` pushes a filter into OmniFocus before data crosses the bridge — use it
instead of fetching everything and filtering in JS.

```js
var tag     = doc.flattenedTags.whose({ name: "backlog" })[0]
var project = doc.flattenedProjects.whose({ name: "My Project" })[0]
var task    = doc.flattenedTasks.whose({ id: "iAKv1Uo8XqW" })[0]
```

`whose()` returns a *specifier*, not an array. Materialise with `()` or index directly:

```js
var tagArray = doc.flattenedTags.whose({ name: "backlog" })()  // real JS array
var firstTag = doc.flattenedTags.whose({ name: "backlog" })[0] // index without materialising
```

`.length` works on a specifier without materialising. Once materialised, use `.filter()` —
`whose()` only works on specifiers.

### Performance: the scripting bridge tax

Every property access crosses the bridge. The key rule: minimise the number of objects
you ask OmniFocus to hand you.

#### Measured timings (real database, 3 matching tasks)

| Approach | Time |
|---|---|
| `flattenedTasks()` → filter all in JS | ~24s |
| `flattenedTasks.whose({completed:false})()` → filter in JS | ~9s |
| `ofTag.tasks()` → filter in JS | ~0.35s |
| `evaluateJavascript()` with OmniJS `tagsMatching().tasks` | ~0.15s |

#### Anti-pattern: fetching everything

```js
// DON'T — materialises every task across the bridge
var tasks = doc.flattenedTasks()
    .filter(function(t) { return t.completed() === false })
    .filter(function(t) {
        return t.tags().some(function(tag) { return tag.id() === ofTag.id() })
    })
```

#### Fast pattern: start narrow

```js
// Start from the tag — only fetches tasks that actually have it (~0.35s)
var tasks = ofTag.tasks()
    .filter(function(t) { return t.completed() === false })
    .map(function(t) { return { id: t.id(), name: t.name() } })
```

Same principle: prefer `project.tasks()` over `flattenedTasks`, prefer
`doc.inboxTasks()` over `flattenedTasks` for inbox items.

---

## OmniJS context (`evaluateJavascript`)

OmniJS runs inside OmniFocus via `app.evaluateJavascript(script)`. It is faster
than JXA for filtered queries because no data crosses the bridge until you return.

Key differences from JXA:
- Properties are **plain fields**, no `()` required: `t.name`, not `t.name()`
- IDs use `t.id.primaryKey` (a string), not `t.id()`
- `t.added` is a timestamp string for task creation time
- Use `Task.Status` enum to filter active tasks (not `completed` boolean)

### Boilerplate

```js
ObjC.import('stdlib');
var args = JSON.parse($.getenv('OSA_ARGS'));
// @ts-ignore
var ofApp = Application("OmniFocus");

var script = `
    // OmniJS code here
    JSON.stringify(result);
`;
ofApp.evaluateJavascript(script);
```

### Look up by ID

```js
// Task
let t = Task.byIdentifier(id)      // returns null if not found
if (t === null) throw new Error("task not found: " + id)

// Project
let proj = Project.byIdentifier(id) // returns null if not found
if (proj === null) throw new Error("project not found: " + id)
```

### Filter active tasks

Use `Task.Status` instead of checking `completed`. Active statuses:

```js
let activeStates = [
    Task.Status.Available,
    Task.Status.DueSoon,
    Task.Status.Next,
    Task.Status.Overdue,
    Task.Status.Blocked
];
let tasks = proj.tasks.filter(function(t) {
    return activeStates.includes(t.taskStatus);
});
```

### `tagsMatching()` — fast tag lookup

`tagsMatching(name)` is the OmniJS equivalent of `flattenedTags.whose({name:...})`.
Starting from a tag's task list and filtering in OmniJS avoids all per-property bridge
crossings (~0.15s vs ~0.35s for JXA `ofTag.tasks()`):

```js
var script = `
JSON.stringify(tagsMatching(${JSON.stringify(args.tag)})[0].tasks.filter(
    function(t) {
        if ([Task.Status.Completed, Task.Status.Dropped].includes(t.taskStatus)) {
            return false
        }
        return t.containingProject &&
               t.containingProject.id.primaryKey === ${JSON.stringify(args.projectId)}
    })
    .map(function(t) {
        return { id: t.id.primaryKey, name: t.name, note: t.note }
    }))`;
ofApp.evaluateJavascript(script);
```

---

## Tasks

### Reading

```js
// Inbox tasks (always filter — includes completed and dropped)
var tasks = doc.inboxTasks().filter(function(t) {
    return !t.completed() && !t.dropped()
}).map(function(t) {
    return { id: t.id(), name: t.name(), note: t.note(),
             tags: t.tags().map(function(tg) { return tg.name() }) }
})

// All tasks across all projects (flattenedTasks includes inbox tasks)
var tasks = doc.flattenedTasks().filter(function(t) {
    return !t.completed() && !t.dropped()
}).map(function(t) {
    var proj = t.containingProject()  // null for inbox tasks — guard it
    return { id: t.id(), name: t.name(), note: t.note(),
             project: proj ? proj.name() : null,
             tags: t.tags().map(function(tg) { return tg.name() }) }
})
```

### Creating

```js
// Inbox task
var task = app.InboxTask({ name: "Buy oat milk" })
doc.inboxTasks.push(task)      // no () on the collection when pushing

// Task in a project (add tags afterwards)
var task = app.Task({ name: "Do the thing", note: "details", dueDate: new Date(2026, 2, 20) })
project.tasks.unshift(task)    // unshift adds to top of project
app.add(ofTag, { to: task.tags })
```

### Completing, undoing, dropping, deleting

```js
// Use markComplete(), not task.completed = true — handles repeating tasks correctly
app.markComplete(task)

// markIncomplete() reverses a completion; completed tasks are still findable by ID
app.markIncomplete(doc.flattenedTasks.whose({ id: taskId })[0])

// Drop (soft delete — hidden from views but preserved in database)
app.markDropped(task)

// Permanent delete — capture id/name first, object is invalid after deletion
var id = task.id(), name = task.name()
app.delete(task)
```

### Updating

```js
// JXA — mutate properties directly
var task = doc.flattenedTasks.whose({ id: "iAKv1Uo8XqW" })[0]
task.name    = "Updated name"
task.flagged = true
task.note    = "Added more context"
task.dueDate = new Date(2026, 3, 1)   // April 1 2026

// OmniJS — same pattern via Task.byIdentifier
let t = Task.byIdentifier(id)
if (t === null) throw new Error("task not found")
t.name = newName
t.note = newNote
```

---

## Tags

Always use `doc.flattenedTags` (not `doc.tags`, which is top-level only).

Tags with ` : ` separators (e.g. `"kanban : backlog"`) create visual grouping in the
UI, but `flattenedTags` returns children by their leaf name (`"backlog"`). Use the
leaf name in `whose()` queries.

```js
// Look up
var ofTag = doc.flattenedTags.whose({ name: "backlog" })[0]

// Create if missing
function tagFoundOrCreated(tagName) {
    var tags = doc.flattenedTags.whose({ name: tagName })
    if (tags.length === 0) {
        var newTag = app.Tag({ name: tagName })
        doc.tags.push(newTag)
        return newTag
    }
    return tags()[0]
}

// Add / remove / swap
app.add(ofTag, { to: task.tags })
app.remove(ofTag, { from: task.tags })

// Read
var names  = task.tags().map(function(tg) { return tg.name() })
var hasTag = task.tags().some(function(tag) { return tag.id() === ofTag.id() })
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

## Projects

```js
// Read all (p.status() can throw on system projects — wrap in try/catch)
var projects = doc.flattenedProjects().map(function(p) {
    var status
    try { status = p.status() } catch(e) { status = "active" }
    var folder = p.folder()
    return { id: p.id(), name: p.name(), status: status,
             folder: folder ? folder.name() : null, note: p.note() }
})

// Create
var proj = app.Project({ name: "Website Redesign" })
doc.projects.push(proj)

// Create inside a folder
var folders = doc.flattenedFolders.whose({ name: "Work" })()
folders[0].projects.push(app.Project({ name: "Q2 OKRs" }))
```

---

## Quick reference

| Goal | Code |
|------|------|
| Get document | `var doc = Application("OmniFocus").defaultDocument` |
| Inbox tasks | `doc.inboxTasks()` |
| All tasks (recursive) | `doc.flattenedTasks()` |
| Tag's tasks | `ofTag.tasks()` |
| Project's tasks | `project.tasks()` |
| All projects | `doc.flattenedProjects()` |
| All tags | `doc.flattenedTags()` |
| Find task by ID (JXA) | `doc.flattenedTasks.whose({ id: "abc" })[0]` |
| Find task by ID (OmniJS) | `Task.byIdentifier("abc")` — null if not found |
| Find project by ID (OmniJS) | `Project.byIdentifier("abc")` — null if not found |
| Find tag by name | `doc.flattenedTags.whose({ name: "backlog" })[0]` |
| Add inbox task | `var t = app.InboxTask({name:"..."}); doc.inboxTasks.push(t)` |
| Add task to project | `project.tasks.unshift(task)` |
| Create project | `var p = app.Project({name:"..."}); doc.projects.push(p)` |
| Add tag to task | `app.add(ofTag, { to: task.tags })` |
| Remove tag from task | `app.remove(ofTag, { from: task.tags })` |
| Complete task | `app.markComplete(task)` |
| Undo completion | `app.markIncomplete(task)` |
| Drop task | `app.markDropped(task)` |
| Delete task | `app.delete(task)` |
| Set due date | `task.dueDate = new Date(2026, 2, 20)` (month 0-indexed) |
| Clear due date | `task.dueDate = null` |
| Flag task | `task.flagged = true` |
| Task's project | `task.containingProject() ? task.containingProject().name() : null` |
| Task's tag names | `task.tags().map(function(tg){ return tg.name() })` |
| Project's folder | `proj.folder() ? proj.folder().name() : null` |
