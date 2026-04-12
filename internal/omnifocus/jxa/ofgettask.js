// @ts-check
// Return a single task by ID.
// Accepts { "id": "abc123" } via OSA_ARGS.
//
// Call it:
//   set -gx OSA_ARGS '{"id":"abc123"}'
//   osascript -l JavaScript ofgettask.js | jq .
(() => {
  "use strict";

  ObjC.import('stdlib')
  const argsJson = $.getenv('OSA_ARGS')

  const script = (jsonString) => {
    const args = JSON.parse(jsonString)
    const t = Task.byIdentifier(args.id)
    if (t === null) throw new Error("task not found: " + args.id)
    return JSON.stringify({
      id:    t.id.primaryKey,
      name:  t.name,
      note:  t.note,
      added: t.added,
      tags:  t.tags.map(tg => tg.name)
    })
  }

  return Application("OmniFocus").evaluateJavascript(
    `(${script})(${JSON.stringify(argsJson)})`
  )
})()
