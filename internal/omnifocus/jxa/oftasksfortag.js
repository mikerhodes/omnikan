// @ts-check
// Return all incomplete tasks that have a given tag and belong to a given project.
// Accepts { "tag": "backlog", "projectId": "abc123" } via OSA_ARGS.
//
// Call it:
//   set -gx OSA_ARGS '{"tag":"backlog","projectId":"abc123"}'
//   osascript -l JavaScript oftasksfortag.js | jq .
(() => {
  "use strict";

  ObjC.import('stdlib')
  const argsJson = $.getenv('OSA_ARGS')

  const script = (jsonString) => {
    const args = JSON.parse(jsonString)
    const tag = flattenedTags.byName(args.tag)
    if (!tag) throw new Error("tag not found: " + args.tag)

    const activeStates = [
      Task.Status.Available,
      Task.Status.DueSoon,
      Task.Status.Next,
      Task.Status.Overdue,
      Task.Status.Blocked
    ]
    return JSON.stringify(tag.tasks
      .filter(t => activeStates.includes(t.taskStatus))
      .filter(t => {
        return t.containingProject &&
          t.containingProject.id.primaryKey === args.projectId
      }).map(t => ({
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
