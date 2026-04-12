// Create a new task in the given project with the given tag.
// Accepts { "name": "...", "tag": "backlog", "projectId": "abc123" } via OSA_ARGS.
//
// Call it:
//   set -gx OSA_ARGS '{"name":"My task","tag":"backlog","projectId":"hblBuJPE81r"}'
//   osascript -l JavaScript ofaddtask.js | jq .
//
// Returns { "id": "...", "name": "...", "note": "" }
//
(() => {
  "use strict";

  ObjC.import('stdlib')
  const argsJson = $.getenv('OSA_ARGS')

  const script = (jsonArgs) => {
    const args = JSON.parse(jsonArgs)

    const project = Project.byIdentifier(args.projectId);
    const tag = flattenedTags.byName(args.tag) || new Tag(args.tag);

    let task = new Task(args.name, project);
    task.addTag(tag);

    return JSON.stringify({
      id: task.id.primaryKey,
      name: task.name,
      note: task.note,
      tags: task.tags.map(tg => tg.name)
    })
  }

  return Application("OmniFocus").evaluateJavascript(
    `(${script})(${JSON.stringify(argsJson)})`
  )
})()
