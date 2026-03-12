# JXA Notes: Scripting OmniFocus from Go

Notes from building omnikan and github-to-omnifocus. JXA
(JavaScript for Automation) is Apple's replacement for AppleScript,
available since OS X Yosemite. Documentation is sparse; these notes
capture what actually works.

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

The last evaluated expression in the script is printed to stdout. Use
`JSON.stringify(result)` as the final line to produce output.

---

## Connecting to OmniFocus

```js
var ofApp = Application("OmniFocus")
var ofDoc = ofApp.defaultDocument
```

`defaultDocument` is the main OmniFocus database. All queries start here.

---

## Reading properties

OmniFocus objects expose their properties as **zero-argument functions**,
not plain properties. You must call them:

```js
task.name()       // "Buy milk"
task.id()         // "iAKv1Uo8XqW"
task.completed()  // false
task.tags()       // [Tag, Tag, ...]
```

This trips you up constantly when first writing JXA. If you write
`task.name` you get a specifier object, not a string.

---

## whose() — server-side filtering

`whose()` is OmniFocus's built-in filter. It runs inside OmniFocus
before any data crosses the scripting bridge, so it is fast.

```js
// Find a tag by name
var tags = ofDoc.flattenedTags.whose({ name: "backlog" })

// Find a project by name
var project = ofDoc.flattenedProjects.whose({ name: "My Project" })[0]

// Find a task by ID
var task = ofDoc.flattenedTasks.whose({ id: "iAKv1Uo8XqW" })[0]

// Find incomplete tasks (see performance section for caveats)
var incomplete = ofDoc.flattenedTasks.whose({ completed: false })
```

`whose()` returns a specifier, not an array. Call it `()` to
materialise the array:

```js
var tagArray = ofDoc.flattenedTags.whose({ name: "backlog" })()
// tagArray is now a real JS array

// Or index directly without materialising:
var firstTag = ofDoc.flattenedTags.whose({ name: "backlog" })[0]
```

**Important:** checking `.length` on a specifier works without
materialising it:

```js
if (ofDoc.flattenedTags.whose({ name: args.tag }).length === 0) {
    // tag doesn't exist
}
```

---

## Performance: the scripting bridge tax

Every property access on an OmniFocus object crosses the scripting
bridge. This is slow. The key rule: **minimise the number of objects
you ask OmniFocus to hand you**.

### Measured timings (real database, 3 matching tasks)

| Approach | Time |
|---|---|
| `flattenedTasks()` → filter all in JS | ~24s |
| `flattenedTasks.whose({completed:false})()` → filter in JS | ~9s |
| `ofTag.tasks()` → filter in JS | ~0.35s |

### The slow anti-pattern: fetching everything

```js
// DON'T DO THIS — fetches every task in the database across the bridge
var tasks = ofDoc.flattenedTasks()
    .filter(function(t) { return t.completed() === false })
    .filter(function(t) {
        return t.tags().some(function(tag) { return tag.id() === ofTag.id() })
    })
```

`flattenedTasks()` materialises every task object. Each `t.completed()`
and `t.tags()` call is another bridge crossing. On a large database
(thousands of tasks) this takes 20+ seconds.

### The fast pattern: start narrow

Start from the most specific collection you can, then fetch only what
you need:

```js
// Start from the tag — only fetches tasks that actually have it
var tasks = ofTag.tasks()
    .filter(function(t) { return t.completed() === false })
    .map(function(t) { return { id: t.id(), name: t.name() } })
```

`ofTag.tasks()` asks OmniFocus to return only the tasks bearing that
tag. For a typical kanban tag with a handful of tasks, this is ~0.35s
vs ~24s for the full scan.

The same principle applies elsewhere:

```js
// Prefer project.tasks() over scanning flattenedTasks for a project
var tasks = project.tasks()

// Prefer ofDoc.inboxTasks() to fetch inbox items
var inbox = ofDoc.inboxTasks()
```

---

## Tags

### Look up a tag by name

```js
var matchingTags = ofDoc.flattenedTags.whose({ name: "backlog" })
if (matchingTags.length === 0) {
    // handle missing tag
} else {
    var ofTag = matchingTags()[0]
}
```

`flattenedTags` searches the entire tag hierarchy, including subtags.
Tag names in OmniFocus can use ` : ` as a visual grouping separator
(e.g. `"kanban : backlog"`). This is just a naming convention — the
full string including spaces and colon is the tag name.

### Create a tag if it doesn't exist

```js
function tagFoundOrCreated(tagName) {
    var tags = ofDoc.flattenedTags.whose({ name: tagName })
    if (tags.length === 0) {
        var newTag = ofApp.Tag({ name: tagName })
        ofDoc.tags.push(newTag)
        return newTag
    }
    return tags()[0]
}
```

### Add a tag to a task

```js
ofApp.add(ofTag, { to: task.tags })
```

### Read tags on a task

```js
var taskTags = task.tags()   // array of Tag objects
var hasTag = taskTags.some(function(tag) {
    return tag.id() === ofTag.id()
})
```

---

## Tasks

### Read task properties

```js
var id   = task.id()
var name = task.name()
var done = task.completed()
var note = task.note()
var due  = task.dueDate()    // JS Date or null
```

### Create a task in a project

```js
var task = ofApp.Task({
    name: "Do the thing",
    note: "some details",
    dueDate: new Date(dueDateMS),  // or null
})
project.tasks.unshift(task)   // adds to top of project
```

### Add a task to the inbox

```js
ofDoc.inboxTasks.push(task)
```

### Mark a task complete

Use `ofApp.markComplete()`, not setting `task.completed = true`:

```js
var task = ofDoc.flattenedTasks.whose({ id: taskId })[0]
ofApp.markComplete(task)
```

---

## Returning data to Go

The last expression evaluated in the script is printed to stdout.
Always use `JSON.stringify()`:

```js
// Return an array
JSON.stringify(tasks)

// Return an object
JSON.stringify({ id: task.id(), name: task.name() })

// Return nothing meaningful
JSON.stringify(null)
```

Go then `json.Unmarshal`s the output into the appropriate struct.

---

## Testing scripts manually

Set `OSA_ARGS` and pipe through `jq`:

```sh
# fish shell
set -gx OSA_ARGS '{"tag": "backlog"}'
osascript -l JavaScript oftasksfortag.js | jq .

# bash/zsh
OSA_ARGS='{"tag": "backlog"}' osascript -l JavaScript oftasksfortag.js | jq .
```

Scripts can also be run without `OSA_ARGS` if you hardcode values for
quick manual testing.

---

## Gotchas

- **OmniFocus must be running.** `Application("OmniFocus")` will launch
  it if it isn't, but this adds latency and can cause the script to fail
  if OmniFocus takes too long to open its database.

- **The scripting bridge is single-threaded.** Concurrent `osascript`
  invocations targeting OmniFocus will contend or deadlock. Make calls
  sequentially from Go.

- **`// @ts-ignore` is needed** if you're editing in VS Code with a
  TypeScript language server. `Application()` is not a standard JS
  global and the type checker complains.

- **`whose()` on a specifier vs a materialised array.** `whose()` only
  works on specifiers (before calling `()`). Once you materialise with
  `()` you have a plain JS array and must use `.filter()`.

- **Tag hierarchy.** `ofDoc.flattenedTags` includes all tags at all
  levels. `ofDoc.tags` is only top-level tags. Use `flattenedTags` for
  lookups unless you specifically need the hierarchy.
