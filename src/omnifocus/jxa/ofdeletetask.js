// Delete a task permanently from OmniFocus.
// Accepts { "id": "..." } via OSA_ARGS.
(() => {
  "use strict";

  ObjC.import('stdlib')
  const argsJson = $.getenv('OSA_ARGS')

  const script = (jsonArgs) => {
    const args = JSON.parse(jsonArgs);
    const task = Task.byIdentifier(args.id);
    deleteObject(task);
    return JSON.stringify({ id: args.id })
  }

  return Application("OmniFocus").evaluateJavascript(
    `(${script})(${JSON.stringify(argsJson)})`
  )
})()
