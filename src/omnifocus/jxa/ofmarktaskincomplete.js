// @ts-check
// Mark a task incomplete in OmniFocus.
// Accepts { "id": "..." } via OSA_ARGS.
//
// Call it:
//   set -gx OSA_ARGS '{"id":"abc123"}'
//   osascript -l JavaScript ofmarktaskincomplete.js
(() => {
  "use strict";

  ObjC.import('stdlib')
  const argsJson = $.getenv('OSA_ARGS')

  const script = (jsonString) => {
    const args = JSON.parse(jsonString)
    const t = Task.byIdentifier(args.id)
    if (t === null) throw new Error("task not found: " + args.id)
    t.markIncomplete()
    return JSON.stringify({ id: t.id.primaryKey, name: t.name })
  }

  return Application("OmniFocus").evaluateJavascript(
    `(${script})(${JSON.stringify(argsJson)})`
  )
})()
