# JXA Notes: Scripting OmniFocus

Notes from building omnikan and github-to-omnifocus. JXA
(JavaScript for Automation) is Apple's scripting bridge available
since OS X Yosemite. Documentation is sparse and inconsistent;
these notes record what actually works.

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

---

## Running JXA from Go

Pass the script via stdin to `osascript -l JavaScript`. Pass arguments
via the `OSA_ARGS` environment variable as a JSON string. The script
writes its output to stdout as JSON.

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
ObjC.import('stdlib')
var args = JSON.parse($.getenv('OSA_ARGS'))
// args.tag, args.projectName, etc.
```

The OmniFocus scripting bridge is single-threaded. Concurrent
`osascript` invocations targeting OmniFocus will contend or deadlock.
Make calls sequentially from Go.

---

## Boilerplate: connecting to OmniFocus

```js
var app = Application("OmniFocus")
var doc = app.defaultDocument
```

`defaultDocument` is the main OmniFocus database. All queries start here.

OmniFocus must be running. JXA cannot launch it headlessly for automation
(it needs a UI session). Launching it adds latency and can cause the
script to fail if the database takes too long to open.

---

## Properties require `()`

OmniFocus objects expose properties as zero-argument functions, not plain
properties. You must call them:

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

---

## whose() — OmniFocus-side filtering

`whose()` pushes a filter query into OmniFocus before any data crosses
the scripting bridge. It is the primary performance tool.

```js
// Find a tag by name
var tags = doc.flattenedTags.whose({ name: "backlog" })

// Find a project by name
var project = doc.flattenedProjects.whose({ name: "My Project" })[0]

// Find a task by ID
var task = doc.flattenedTasks.whose({ id: "iAKv1Uo8XqW" })[0]

// Find incomplete tasks
var incomplete = doc.flattenedTasks.whose({ completed: false })
```

`whose()` returns a *specifier*, not an array. Call it `()` to
materialise the array:

```js
var tagArray = doc.flattenedTags.whose({ name: "backlog" })()
// tagArray is now a real JS array

// Or index directly without materialising:
var firstTag = doc.flattenedTags.whose({ name: "backlog" })[0]
```

Checking `.length` on a specifier works without materialising it:

```js
if (doc.flattenedTags.whose({ name: "kanban" }).length === 0) {
    // tag doesn't exist
}
```

Once you materialise with `()` you have a plain JS array — use `.filter()`
from there. `whose()` only works on specifiers.

---

## Performance: the scripting bridge tax

Every property access on an OmniFocus object crosses the scripting
bridge. This is slow. The key rule: minimise the number of objects you
ask OmniFocus to hand you.

### Measured timings (real database, 3 matching tasks)

| Approach | Time |
|---|---|
| `flattenedTasks()` → filter all in JS | ~24s |
| `flattenedTasks.whose({completed:false})()` → filter in JS | ~9s |
| `ofTag.tasks()` → filter in JS | ~0.35s |

### The slow anti-pattern: fetching everything

```js
// DON'T DO THIS — materialises every task in the database across the bridge
var tasks = doc.flattenedTasks()
    .filter(function(t) { return t.completed() === false })
    .filter(function(t) {
        return t.tags().some(function(tag) { return tag.id() === ofTag.id() })
    })
```

`flattenedTasks()` fetches every task. Each `t.completed()` and
`t.tags()` call is another bridge crossing. On a large database
(thousands of tasks) this takes 20+ seconds.

### The fast pattern: start narrow

Start from the most specific collection you can:

```js
// Start from the tag — only fetches tasks that actually have it
var tasks = ofTag.tasks()
    .filter(function(t) { return t.completed() === false })
    .map(function(t) { return { id: t.id(), name: t.name() } })
```

`ofTag.tasks()` asks OmniFocus to return only the tasks bearing that tag.
For a typical kanban tag with a handful of tasks this is ~0.35s vs ~24s
for the full scan.

Same principle elsewhere:

```js
// Prefer project.tasks() over scanning flattenedTasks
var tasks = project.tasks()

// Prefer doc.inboxTasks() to fetch inbox items
var inbox = doc.inboxTasks()
```

---

## Reading tasks

### Inbox tasks

```js
var tasks = doc.inboxTasks().filter(function(t) {
    return !t.completed() && !t.dropped()
}).map(function(t) {
    return {
        id:        t.id(),
        name:      t.name(),
        flagged:   t.flagged(),
        note:      t.note(),
        tags:      t.tags().map(function(tg) { return tg.name() })
    }
})
JSON.stringify(tasks)
```

`doc.inboxTasks()` includes completed and dropped tasks — always filter.

### All tasks across all projects

```js
var tasks = doc.flattenedTasks().filter(function(t) {
    return !t.completed() && !t.dropped()
}).map(function(t) {
    var proj = t.containingProject()
    return {
        id:        t.id(),
        name:      t.name(),
        flagged:   t.flagged(),
        dueDate:   t.dueDate() ? t.dueDate().toISOString() : null,
        note:      t.note(),
        project:   proj ? proj.name() : null,
        tags:      t.tags().map(function(tg) { return tg.name() })
    }
})
JSON.stringify(tasks)
```

`flattenedTasks()` includes inbox tasks. When combining with
`inboxTasks()`, deduplicate by ID to avoid doubles.

`containingProject()` returns `null` for inbox tasks — guard it.

### Tasks for a tag (fast approach)

```js
ObjC.import('stdlib')
var args = JSON.parse($.getenv('OSA_ARGS'))   // { "tag": "backlog" }

var app = Application("OmniFocus")
var doc = app.defaultDocument

var matchingTags = doc.flattenedTags.whose({ name: args.tag })
if (matchingTags.length === 0) {
    JSON.stringify([])
} else {
    var ofTag = matchingTags()[0]
    var tasks = ofTag.tasks()
        .filter(function(t) { return t.completed() === false })
        .map(function(t) { return { id: t.id(), name: t.name() } })
    JSON.stringify(tasks)
}
```

### Tasks for a project with a specific tag

```js
var project = doc.flattenedProjects.whose({ name: "My Project" })[0]
var ofTag   = doc.flattenedTags.whose({ name: "github" })()[0]

var tasks = project.tasks()
    .filter(function(t) { return t.completed() === false })
    .filter(function(t) {
        return t.tags().some(function(tag) { return tag.id() === ofTag.id() })
    })
    .map(function(t) { return { id: t.id(), name: t.name() } })

JSON.stringify(tasks)
```

---

## Dates

### Reading dates

`dueDate()` returns a JS `Date` object or `null`.

```js
var due = t.dueDate()   // Date or null
```

### Avoiding timezone shifts with toISOString()

OmniFocus stores due dates as midnight local time. `toISOString()` shifts
to UTC, making "2026-03-15" appear as "2026-03-14T23:00:00.000Z" in a
UTC+1 timezone. Format manually to preserve local date:

```js
function toLocalDateString(d) {
    if (!d) return null
    var year  = d.getFullYear()
    var month = String(d.getMonth() + 1).padStart(2, "0")
    var day   = String(d.getDate()).padStart(2, "0")
    return year + "-" + month + "-" + day
}
// e.g. toLocalDateString(t.dueDate()) → "2026-03-15"
```

### Setting due dates

`new Date("2026-03-20")` parses as UTC midnight and will be off by your
timezone offset. Always construct from year/month/day components:

```js
// ✅ Correct — local midnight
task.dueDate = new Date(2026, 2, 20)   // month is 0-indexed: 2 = March

// ❌ Wrong — parses as UTC, shifts in non-UTC timezones
task.dueDate = new Date("2026-03-20")
```

To clear a due date:

```js
task.dueDate = null
```

---

## Tags

### Look up a tag

`flattenedTags` searches the entire hierarchy including subtags. Tags in
OmniFocus can use ` : ` as a visual grouping separator (e.g.
`"kanban : backlog"`) — this is just a naming convention; the full string
is the tag name.

```js
var matchingTags = doc.flattenedTags.whose({ name: "backlog" })
if (matchingTags.length === 0) {
    // tag doesn't exist
} else {
    var ofTag = matchingTags()[0]
}
```

`doc.tags` is top-level tags only. Use `doc.flattenedTags` for lookups.

The ` : ` separator in tag names (e.g. `"kanban : backlog"`) creates a
visual parent/child grouping in OmniFocus's UI, but `flattenedTags()`
returns the children by their short leaf name (`"backlog"`), not the
full path. Confirmed by listing all tags:

```js
doc.flattenedTags().map(function(t) { return t.name() })
// ["kanban", "backlog", "ready", "inprogress", "done", ...]
```

So when querying by name, use the leaf name: `whose({ name: "backlog" })`,
not `whose({ name: "kanban : backlog" })`.

### Create a tag if it doesn't exist

```js
function tagFoundOrCreated(tagName) {
    var tags = doc.flattenedTags.whose({ name: tagName })
    if (tags.length === 0) {
        var newTag = app.Tag({ name: tagName })
        doc.tags.push(newTag)
        return newTag
    }
    return tags()[0]
}
```

### Add a tag to a task

```js
app.add(ofTag, { to: task.tags })
```

### Remove a tag from a task

```js
app.remove(ofTag, { from: task.tags })
```

### Swap one tag for another

```js
var oldTag = doc.flattenedTags.whose({ name: "backlog" })[0]
var newTag = doc.flattenedTags.whose({ name: "inprogress" })[0]

app.remove(oldTag, { from: task.tags })
app.add(newTag, { to: task.tags })
```

`app.remove` / `app.add` are mirrors of each other. This is how you
move a task between Kanban columns.

### Read tags on a task

```js
var names  = task.tags().map(function(tg) { return tg.name() })
var hasTag = task.tags().some(function(tag) { return tag.id() === ofTag.id() })
```

---

## Creating tasks

### Add a task to the inbox

```js
var task = app.InboxTask({ name: "Buy oat milk" })
doc.inboxTasks.push(task)
JSON.stringify({ id: task.id(), name: task.name() })
```

Task objects are constructed from `app`, then pushed onto the document
collection. No `()` on the collection when pushing.

### With note, flag, and due date

```js
var task = app.InboxTask({ name: "Prepare sprint review" })
doc.inboxTasks.push(task)
task.note    = "Cover velocity, blockers, and Q2 roadmap"
task.flagged = true
task.dueDate = new Date(2026, 2, 20)   // March 20 2026
JSON.stringify({ id: task.id(), name: task.name() })
```

### Add a task to a project

```js
var task = app.Task({
    name:    "Do the thing",
    note:    "some details",
    dueDate: new Date(2026, 2, 20),   // or null
})
project.tasks.unshift(task)   // adds to top of project
```

Add tags afterwards:

```js
app.add(ofTag, { to: task.tags })
```

---

## Completing, dropping, and deleting tasks

### Complete a task

Use `app.markComplete()`, not `task.completed = true`. The method also
handles repeating tasks correctly.

```js
var task = doc.flattenedTasks.whose({ id: taskId })[0]
app.markComplete(task)
```

### Undo a completion (mark incomplete)

`app.markIncomplete()` reverses a completion. Confirmed working in testing.

```js
var task = doc.flattenedTasks.whose({ id: taskId })[0]
app.markIncomplete(task)
```

Note: completed tasks are still returned by `flattenedTasks.whose({ id: ... })`,
so you can look them up by ID and un-complete them immediately after completion.

### Drop a task (soft delete)

Dropped tasks are hidden from normal views but preserved in the database.

```js
app.markDropped(task)
```

### Delete a task permanently

```js
var name = task.name()   // capture before deletion — object becomes invalid after
var id   = task.id()
app.delete(task)
JSON.stringify({ id: id, name: name, deleted: true })
```

---

## Updating a task

Mutate properties directly on a fetched task object:

```js
var task = doc.flattenedTasks.whose({ id: "iAKv1Uo8XqW" })[0]
task.name    = "Updated name"
task.flagged = true
task.note    = "Added more context"
task.dueDate = new Date(2026, 3, 1)   // April 1 2026
```

---

## Projects

### Read all projects

```js
var projects = doc.flattenedProjects().map(function(p) {
    var status
    try { status = p.status() } catch(e) { status = "active" }
    var folder = p.folder()
    return {
        id:         p.id(),
        name:       p.name(),
        status:     status,
        taskCount:  p.flattenedTasks().length,
        folder:     folder ? folder.name() : null,
        note:       p.note()
    }
})
JSON.stringify(projects)
```

`p.status()` can throw on system-generated projects (like the inbox
project) — wrap in try/catch.

### Create a project

```js
var proj = app.Project({ name: "Website Redesign" })
doc.projects.push(proj)
JSON.stringify({ id: proj.id(), name: proj.name() })
```

### Create a project inside a folder

```js
var folders = doc.flattenedFolders.whose({ name: "Work" })()
if (folders.length === 0) {
    JSON.stringify({ error: "Folder not found" })
} else {
    var proj = app.Project({ name: "Q2 OKRs" })
    folders[0].projects.push(proj)
    JSON.stringify({ id: proj.id(), name: proj.name() })
}
```

---

## Folders

```js
var folders = doc.flattenedFolders().map(function(f) {
    return {
        id:           f.id(),
        name:         f.name(),
        projectCount: f.projects().length
    }
})
JSON.stringify(folders)
```

---

## Searching tasks by text

```js
var seen = {}
var all  = []

doc.flattenedTasks().forEach(function(t) {
    if (!seen[t.id()]) { seen[t.id()] = true; all.push(t) }
})
doc.inboxTasks().forEach(function(t) {
    if (!seen[t.id()]) { seen[t.id()] = true; all.push(t) }
})

var q = "deploy".toLowerCase()
var matches = all.filter(function(t) {
    return t.name().toLowerCase().indexOf(q) !== -1
        || (t.note() && t.note().toLowerCase().indexOf(q) !== -1)
}).map(function(t) {
    return { id: t.id(), name: t.name() }
})

JSON.stringify(matches)
```

---

## Quick reference

| Goal | JXA |
|------|-----|
| Get document | `var doc = Application("OmniFocus").defaultDocument` |
| Inbox tasks | `doc.inboxTasks()` |
| All tasks (recursive) | `doc.flattenedTasks()` |
| Tag's tasks (fast) | `ofTag.tasks()` |
| Project's tasks | `project.tasks()` |
| All projects | `doc.flattenedProjects()` |
| All tags | `doc.flattenedTags()` |
| All folders | `doc.flattenedFolders()` |
| Find by ID | `doc.flattenedTasks.whose({ id: "abc" })[0]` |
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
