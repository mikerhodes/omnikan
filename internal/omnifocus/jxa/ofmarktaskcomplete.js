// Mark a task complete in OmniFocus.
// Accepts { "id": "..." } via OSA_ARGS.

ObjC.import('stdlib')
var args = JSON.parse($.getenv('OSA_ARGS'))

// @ts-ignore
var app = Application("OmniFocus")
var task = app.defaultDocument.flattenedTasks.whose({ id: args.id })[0]
app.markComplete(task)

JSON.stringify({ id: task.id(), name: task.name() })
