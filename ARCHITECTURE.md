# Architecture Notes

This document covers implementation details that aren't user-facing CLI
behavior (see [README.md](README.md) for that), but matter to anyone working
on `vat` itself — human or LLM.

## On-Disk File Format (`format.go`)

The outer wrapping of a `.vat` file is unchanged: `age`-encrypted, `gzip`-compressed
JSON (see the README's [Working with Encrypted Assessment Files](README.md#working-with-encrypted-assessment-files)
section for the extract/repackage commands, which still apply as-is).

What changed is the shape of the JSON *inside* that wrapping. As of vat 2.0 it's
an envelope rather than a flat struct:

```json
{
  "manifest": {
    "version": "2",
    "vat-version": "...",
    "vectr-version": "...",
    "created": "2026-01-01T00:00:00Z",
    "resources": ["assessment", "librarytestcases", "orgmap", "toolsmap", "idtoolsmap"]
  },
  "data": {
    "assessment": { ... },
    "librarytestcases": { ... },
    "orgmap": { ... },
    "toolsmap": { ... },
    "idtoolsmap": { ... }
  }
}
```

- `manifest` records format version, vat/VECTR provenance, creation time, and
  which resources are present.
- `data` maps resource name to that resource's raw JSON payload. Anyone
  hand-editing an extracted `assessment.json` needs to edit under
  `data.<resource>`, not the top level.

Resources are driven off a single `resourceRegistry` table in `format.go`,
which pairs each resource name with its encode/decode functions and whether
it's required. This is the extension point for adding a new resource to the
format.

**Hard version break:** vat 2.0 refuses to decode vat 1.x's old flat-format
files (`DecodeJson` errors if `Manifest.FormatVersion` is empty or
`Resources` is empty) — there is no auto-upgrade path. See the README's
[Upgrading from vat 1.x](README.md#upgrading-from-vat-1x) section for how to
migrate existing archives.

**The vat 2.x guarantee:** `FormatVersion` is currently `"2"` and is meant to
be a stable target for the entire vat 2.x line — every vat 2.x release commits
to reading any file with manifest version `2`. This works because
`FormatVersion` is round-tripped but not otherwise branched on in code;
compatibility across 2.x releases is instead handled per-resource (see the
compatibility convention documented on the `FormatVersion` constant in
`format.go`): routine field changes don't bump the version at all, and
breaking changes to a resource get a new resource name instead of mutating the
existing one in place, so older 2.x decoders skip what they don't recognize
and keep working. `FormatVersion` only moves again alongside a new major vat
version, which would be its own hard break, the same way vat 2.0 broke
compatibility with vat 1.x.

## Restore Compatibility Model

Restore follows Postel's Law / the robustness principle — "be liberal in
what you accept" — when it encounters data it doesn't recognize, on the
assumption that the *source* of a serialized assessment may be a newer VECTR
version than the *target* `vat` build knows about:

- Unrecognized resource names in the envelope's `data` map are skipped with a
  warning rather than failing the whole restore.
- Unrecognized status, executor, and timeline field-change values are passed
  through as-is with a warning log rather than aborting restore, letting the
  target VECTR instance itself reject the value if it's truly invalid.

This is a deliberate default, not a blanket "ignore everything unknown"
policy. The one explicit exception is a test case with no organization set:
that's never valid VECTR data (not a compatibility gap opened up by version
skew), so restore treats it as fatal rather than passing through a blank
organization.

## Test Case Correlation

Test case creates are correlated back to their source by `clientId` rather
than `libraryTestCaseId`. `libraryTestCaseId` isn't guaranteed unique within a
single restore batch (e.g. two environment test cases derived from the same
library template), which previously caused mismatched correlation; it also
doesn't exist at all for no-template test cases, which meant they silently
never received their timeline events. `clientId` is generated per-test-case by
`vat` for the duration of a single restore call, so it's guaranteed unique
and always present.

## Defense Tool Reconciliation

`reconcileDefenseTools` (`restore.go`) resolves each `DefenseToolRef` in an
assessment against the target instance:

1. Reuse a matching tool, matched on name, product, and active state.
2. If a close match exists but is missing some defense layers referenced by
   the assessment, add the missing layers to it.
3. Otherwise, create the product, layers, and tool from scratch (`FindVendor`,
   `CreateDefenseToolProduct`, `CreateDefenseLayer`, `CreateDefenseTool`
   GraphQL operations).

If more than one tool in the target instance matches, `vat` picks the most
recently updated one and logs a warning — this is a known limitation, not a
guarantee of correctness, since VECTR doesn't enforce uniqueness on
name+product+active-state.

Defense tool data that's missing something reconciliation needs to safely act
(e.g. a blank name) fails restore outright (`ErrIncompleteDefenseToolData`)
rather than creating a broken record in VECTR.
