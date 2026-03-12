// Swap one kanban tag for another on a task.
// Accepts { "id": "...", "oldTag": "backlog", "newTag": "inprogress" } via OSA_ARGS.
//
// Call it:
//   set -gx OSA_ARGS '{"id":"abc123","oldTag":"backlog","newTag":"inprogress"}'
//   osascript -l JavaScript ofswaptag.js

ObjC.import('stdlib')
var args = JSON.parse($.getenv('OSA_ARGS'))

// @ts-ignore
var app = Application("OmniFocus")
var doc = app.defaultDocument

var task   = doc.flattenedTasks.whose({ id: args.id })[0]
var oldTag = doc.flattenedTags.whose({ name: args.oldTag })[0]
var newTag = doc.flattenedTags.whose({ name: args.newTag })[0]

app.remove(oldTag, { from: task.tags })
app.add(newTag, { to: task.tags })

JSON.stringify({ id: task.id(), name: task.name(), tags: task.tags().map(function(t) { return t.name() }) })
