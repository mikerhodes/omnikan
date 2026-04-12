// @ts-check
// Return all incomplete tasks for a project.
// Accepts { "projectid": "abc123" } via OSA_ARGS.
//
// Call it:
//   set -gx OSA_ARGS '{"projectid":"abc123"}'
//   osascript -l JavaScript oftasksforproject.js | jq .
(() => {
  "use strict";

  ObjC.import('stdlib')
  const argsJson = $.getenv('OSA_ARGS')

  const script = (jsonArgs) => {
    const args = JSON.parse(jsonArgs)
    const proj = /** @type {Project} */ Project.byIdentifier(args.projectid)
    if (proj === null) throw new Error("project not found: " + args.projectid)
    const activeStates = [
      Task.Status.Available,
      Task.Status.DueSoon,
      Task.Status.Next,
      Task.Status.Overdue,
      Task.Status.Blocked
    ]
    return JSON.stringify(proj.tasks
      .filter(t => activeStates.includes(t.taskStatus))
      .map(t => ({
        id: t.id.primaryKey,
        name: t.name,
        note: t.note,
        added: t.added,
        tags: t.tags.map(tg => tg.name)
      })))
  }

  return Application("OmniFocus").evaluateJavascript(
    `(${script})(${JSON.stringify(argsJson)})`
  )
})()
