// @ts-check
// Swap one kanban tag for another on a task.
// Accepts { "id": "...", "oldTag": "backlog", "newTag": "inprogress" } via OSA_ARGS.
//
// Call it:
//   set -gx OSA_ARGS '{"id":"abc123","oldTag":"backlog","newTag":"inprogress"}'
//   osascript -l JavaScript ofswaptag.js
(() => {
  "use strict";

  ObjC.import('stdlib')
  const argsJson = $.getenv('OSA_ARGS')

  const script = (jsonString) => {
    const args = JSON.parse(jsonString)
    const t = Task.byIdentifier(args.id)
    if (t === null) throw new Error("task not found: " + args.id)
    const oldTag = flattenedTags.byName(args.oldTag)
    const newTag = flattenedTags.byName(args.newTag)
    if (!oldTag) throw new Error("tag not found: " + args.oldTag)
    if (!newTag) throw new Error("tag not found: " + args.newTag)
    t.removeTag(oldTag)
    t.addTag(newTag)
    return JSON.stringify({
      id: t.id.primaryKey,
      name: t.name,
      tags: t.tags.map(tg => tg.name)
    })
  }

  return Application("OmniFocus").evaluateJavascript(
    `(${script})(${JSON.stringify(argsJson)})`
  )
})()
