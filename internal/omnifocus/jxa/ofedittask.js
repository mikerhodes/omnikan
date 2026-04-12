// @ts-check
// Edit a task's name and note by ID.
// Accepts { "id": "abc123", "name": "New Name", "note": "New Note" } via OSA_ARGS.
//
// Call it:
//   set -gx OSA_ARGS '{"id":"abc123","name":"Updated Name","note":"Updated Note"}'
//   osascript -l JavaScript ofedittask.js | jq .
(() => {
  "use strict";

  ObjC.import('stdlib')
  const argsJson = $.getenv('OSA_ARGS')

  const script = (jsonString) => {
    const args = JSON.parse(jsonString)
    const t = Task.byIdentifier(args.id)
    if (t === null) throw new Error("task not found: " + args.id)
    t.name = args.name
    t.note = args.note
    return JSON.stringify({
      id:   t.id.primaryKey,
      name: t.name,
      note: t.note
    })
  }

  return Application("OmniFocus").evaluateJavascript(
    `(${script})(${JSON.stringify(argsJson)})`
  )
})()
