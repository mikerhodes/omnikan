// @ts-check
// Return the ID of a project by name.
// Accepts { "projectName": "My Project" } via OSA_ARGS.
//
// Call it:
//   set -gx OSA_ARGS '{"projectName":"My Project"}'
//   osascript -l JavaScript ofprojectid.js | jq .
(() => {
  "use strict";

  ObjC.import('stdlib')
  const argsJson = $.getenv('OSA_ARGS')

  const script = (jsonString) => {
    const args = JSON.parse(jsonString)
    const proj = flattenedProjects.byName(args.projectName)
    if (proj === null) throw new Error("project not found: " + args.projectName)
    return JSON.stringify({ id: proj.id.primaryKey })
  }

  return Application("OmniFocus").evaluateJavascript(
    `(${script})(${JSON.stringify(argsJson)})`
  )
})()
