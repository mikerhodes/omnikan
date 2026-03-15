// Create a new task in the given project with the given tag.
// Accepts { "name": "...", "tag": "backlog", "projectId": "abc123" } via OSA_ARGS.
//
// Call it:
//   set -gx OSA_ARGS '{"name":"My task","tag":"backlog","projectId":"hblBuJPE81r"}'
//   osascript -l JavaScript ofaddtask.js | jq .
//
// Returns { "id": "...", "name": "...", "note": "" }

ObjC.import('stdlib')
var args = JSON.parse($.getenv('OSA_ARGS'))

// @ts-ignore
var app = Application("OmniFocus")
var doc = app.defaultDocument

var project = doc.flattenedProjects.whose({ id: args.projectId })[0]
var tag = doc.flattenedTags.whose({ name: args.tag })[0]

var task = app.Task({ name: args.name })
project.tasks.push(task)
app.add(tag, { to: task.tags })

JSON.stringify({ id: task.id(), name: task.name(), note: task.note() })
