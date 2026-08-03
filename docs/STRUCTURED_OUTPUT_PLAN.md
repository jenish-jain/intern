# Migrate Anthropic Responses to Structured Tool-Use Output

## Problem Statement
`PlanChanges`/`FixErrors` in `internal/ai/agent/anthropic/client.go` ask the model to emit
free-text JSON (a `CodeChange` array, or a `{"need_files":[...]}` retrieval request) and then
parse it by hand. This has repeatedly broken in production, each time in a new shape:

- Non-Go files truncated below what the model needed, causing edits against content it never saw.
- `need_files` retrieval only fired for files already labeled "signatures only" - a file absent
  from context entirely wasn't covered, so the model fabricated content instead of asking (fixed
  in PR #32).
- After that fix, the model correctly asked for the unseen file but emitted a bare
  `["path", ...]` array instead of the instructed `{"need_files":[...]}` object, which
  `json.Unmarshal` into `[]agent.CodeChange` rejected outright - deterministically, so retries
  never helped (fixed in PR #33 by widening the parser).
- ~140 lines of `attemptJSONFix`/`fixCommonMalformations`/`attemptSimpleFix`/
  `attemptAggressiveFix`/`isBalanced` exist specifically to paper over cosmetic JSON malformation
  (extra braces, truncated objects) in the model's free-text output.

Each fix so far has been a reactive patch for one specific drift in the model's output shape.
The underlying issue is structural: we're relying on prompt wording to produce a parseable shape,
with no enforcement, so any new drift produces a fresh runtime parse error.

## Proposed Fix
Switch the Anthropic provider from free-text JSON to the Anthropic Messages API's native
tool-use (structured output), which makes the API itself enforce the response schema instead of
hoping the prompt was worded precisely enough.

- Define two tools: `propose_changes` (the `CodeChange` array shape) and `request_files` (the
  `need_files` shape). `tool_choice: {"type":"any"}` forces the model to call one of them - no
  more freeform text, no markdown wrapping, no cosmetic malformation.
- `anthropic/types.go`: add `tools`/`tool_choice` to the request struct; extend the response
  content-block struct to carry `type`/`input` for tool-use blocks.
- `anthropic/client.go`: replace the text-parsing tail of `PlanChanges`/`FixErrors` with
  tool-input parsing. Delete `attemptJSONFix` and friends (~140 lines) - schema validation makes
  them unnecessary.
- `templates.go`: trim now-redundant format instructions (JSON shape examples, "output ONLY a
  compact array", the `need_files` object spec) - the schema enforces shape now. Semantic rules
  (edit only full-content files, verbatim old-blocks, naming-collision handling via `note`) stay.
- New tests covering schema request construction and tool-use response parsing.

## Scope
- Touches `internal/ai/agent/anthropic/client.go`, `internal/ai/agent/anthropic/types.go`,
  `internal/ai/agent/templates.go`, plus a new test file.
- No changes to the `agent.Agent` interface or orchestrator.
- **Ollama is out of scope.** Local models' tool-calling support is inconsistent across
  quantizations/versions, so `ollama/client.go` keeps the current free-text parsing path
  (`agent.ParseNeedFiles`, `agent.SanitizeResponse`) unchanged.
- Estimated: one self-contained PR, roughly half a day.

## Status
Not started. Planned for this weekend (2026-08-08/09).
