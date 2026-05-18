# FLOWDOC.md — `vv flowdoc` internals

> **Browse the full epic as a unified diff:** [github.com/suykerbuyk/vibe-vault — `1428fb1...f2d6545`](https://github.com/suykerbuyk/vibe-vault/compare/1428fb1...f2d6545) (13 commits, +13086/-18 LoC, 63 files; see [Git reference range](#git-reference-range) for the per-commit breakdown).

`vv flowdoc` produces a language-agnostic JSON document (`doc/flows.json`)
plus a rendered HTML viewer (`doc/FLOWS.html`) describing a project's
workflows — slash commands, CLI verbs, hooks, pipeline stages — as a
graph of labeled `Node`s connected by ordered `Step`s. The JSON is the
primary artifact for AI consumption (denser-than-source structural
context); the HTML is the human-facing complement.

Two subcommands ship: `vv flowdoc gen` (LLM-backed generation) and
`vv flowdoc verify` (zero-LLM lint). A third surface — a `flowdoc`
staleness row in `vv check` — surfaces drift in the standard
preflight.

## Source layout

```
internal/flowdoc/
  types.go          schema, enum tables, Validate
  brief.go          generator brief + agentic addendum (Go string consts)
  repo.go           WalkRepo + RepoView (lazy file/search accessors)
  keyfiles.go       SelectKeyFiles (discovery-driven curation)
  context.go        BuildContext (prompt-pack assembly + budget gating)
  tools.go          NewRepoViewTools (agentic tool catalogue + dispatch)
  verify.go         VerifyRefs + ref taxonomy
  viewer.go         Render (HTML)
  viewer.html       embedded template
  testdata/         golden fixture (flows.json + FLOWS.html)

cmd/vv/
  flowdoc.go        runFlowdoc → gen | verify dispatch + finalize pipeline
```

The `cmd/vv` layer is a thin shell around `internal/flowdoc`. All
language-aware logic lives in the package.

## Schema (`$schema_version: "1"`)

`internal/flowdoc/types.go` is the single source of truth. The `FlowDoc`
container holds a `Nodes` set and a `Flows` set. Every `Flow` references
node IDs through its `Nodes []string` membership list and its `Steps
[]Step` where each `Step.From` and `Step.To` resolves to a top-level
node ID.

```go
type FlowDoc struct {
    SchemaVersion string // must be "1"
    Project       string
    GeneratedAt   string // RFC3339
    Generator     string
    Nodes         []Node
    Flows         []Flow
}

type Node struct {
    ID, Label, Path, Language, LayoutGroup, Kind string
    Role string `json:",omitempty"`
}

type Flow struct {
    Slug, Label, Kind, Description, EntryPoint string
    Elided string `json:",omitempty"`
    Nodes  []string
    Steps  []Step
}

type Step struct {
    From, To, Op string
    Passes, Ref  string `json:",omitempty"` // optional
}
```

**Enum tables** (in `types.go`):

| Field           | Values                                                                                       |
| --------------- | -------------------------------------------------------------------------------------------- |
| `Node.Language` | `go`, `rust`, `c`, `cpp`, `python`, `doc`, `data`, `template`, `external`                    |
| `Node.Kind`     | `subsystem`, `binary`, `library`, `service`, `template`, `stage`, `external`                 |
| `Flow.Kind`     | `slash-command`, `cli-verb`, `hook`, `pipeline-stage`                                        |

`Validate` enforces non-empty required fields, enum membership, ID
uniqueness across nodes, slug uniqueness across flows, and that every
`Flow.Nodes[]` / `Step.From` / `Step.To` resolves to a real node ID.

A small `languageAliases` table normalizes common colloquial spellings
(`c++` → `cpp`, `golang` → `go`, `py`/`python3` → `python`, JS/TS
collapse to `data`) before enum-membership checking, so the LLM is not
rejected for emitting `c++`.

## `vv flowdoc gen` — generation pipeline

```
cwd
 │
 ├─ project name  ← --project | session.DetectProject | filepath.Base
 ├─ project root  ← session.DetectProjectRoot (first ancestor with .git)
 │
 ├─ flowdoc.WalkRepo(root)                ← repo introspection (Phase 1)
 │      ↓
 │   RepoView {Files, Budget, Source}
 │      ↓
 ├─ chooseStrategy(opts, cfg)             ← auto: agentic if Anthropic key, else single-shot
 │      ↓
 │   ┌─ single-shot ─────────────────┐  ┌─ agentic ──────────────────────┐
 │   │ SelectKeyFiles + BuildContext │  │ NewRepoViewTools(view)          │
 │   │ → user prompt with tree+files │  │ → 4 tools (read_file/grep/      │
 │   │                               │  │   list_dir/check_ref)           │
 │   │ provider.ChatCompletion(…)    │  │ provider.RunTools(…)            │
 │   │ → raw JSON string             │  │ → ContentBlock[] → text         │
 │   └───────────────────────────────┘  └─────────────────────────────────┘
 │      ↓
 ├─ finalizeFlowdoc(rawJSON, …)
 │      ├─ stripJSONFence + skip-leading-prose tolerance
 │      ├─ json.Decode → FlowDoc
 │      ├─ stamp SchemaVersion / Project / Generator / GeneratedAt defaults
 │      ├─ flowdoc.Validate
 │      ├─ writeFlowsJSON   → <root>/doc/flows.json (indented, trailing \n)
 │      └─ writeFlowsHTML   → <root>/doc/FLOWS.html (embedded template)
 │
 └─ --open → xdg-open / open (fire-and-forget)
```

### Repo introspection (`repo.go`)

`WalkRepo(root)` returns a `RepoView` enumerating the project's kept
source files. Walk source is preferentially `git ls-files`; a
`filepath.WalkDir` fallback covers non-git roots. Three filter classes
apply:

1. **gitignored** — `git ls-files` skips by design.
2. **submodule gitlinks** — `--stage` mode `160000` entries name a
   commit pointer, not a readable file. Explicit drop.
3. **committed-noise** — a denylist (`vendor/`, `dist/`, `build/`,
   `node_modules/`, `target/`, …) catches committed third-party trees
   the language-level filters miss.

`RepoView` exposes lazy accessors:

- `ReadFile(path)` — 1 MiB cap, kept-set membership check.
- `Search(pattern, prefix)` — RE2 grep with optional path prefix.

These accessors back **both** generation strategies. The single-shot
prompt-builder consumes them; the agentic tool catalogue forwards
through to them. Filter semantics are the same on both paths.

`Budget` tracks `FileCount`, `TotalBytes`, and `EstimatedTokens` (a
labelled ~4-char/token heuristic; there is no real tokenizer in the
repo, per design).

### Key-file selection (`keyfiles.go`)

`SelectKeyFiles(view)` is **discovery-driven, not hardcoded**. A small
`langProfiles` table declares per-language manifest basenames and
conventional entry-point basenames:

| Language profile | Manifests                                              | Entry points                        |
| ---------------- | ------------------------------------------------------ | ----------------------------------- |
| go               | `go.mod`, `go.work`                                    | `main.go`                           |
| c-cmake          | `CMakeLists.txt`, `meson.build`, `configure.ac`        | `main.{c,cc,cpp,cxx}`               |
| python           | `pyproject.toml`, `setup.py`, `setup.cfg`              | `__main__.py`, `main.py`, `app.py`  |
| rust             | `Cargo.toml`                                           | `main.rs`                           |
| node             | `package.json`                                         | `index.{js,ts}`, `main.{js,ts}`     |

Plus cross-language manifests (`Makefile`, top-level `README.md`,
`CLAUDE.md`, `AGENTS.md`, etc.).

A file is selected if **any** profile matches its basename. Adding a
language is appending a `langProfile`; the selection loop never
changes.

### Context assembly (`context.go`)

`BuildContext(view, keyFiles, maxBytes)` packs the prompt:

- Full tree listing (always — keeps the model honest about what
  exists).
- Inlined contents of selected key files, in path-sorted order, until
  the byte budget (`DefaultContextBudgetBytes` ≈ 256 KiB, override
  via `--max-context-bytes`) is exhausted.
- A `BuildStats` struct reports `Included` and `Dropped` paths so
  `--dry-run` can show exactly what would be sent.

### Strategy selection (`chooseStrategy`)

`auto` (default) picks **agentic** if an Anthropic API key is
configured, **single-shot** otherwise. Explicit `--strategy
agentic|single-shot` overrides. Rationale: agentic gives the model
control over what it reads, which empirically produces fewer
hallucinated paths on heterogeneous repos; single-shot is the
zero-tool-use fallback for any provider (Grok, OpenAI, etc.).

### Single-shot path (`runFlowdocSingleShotLLM`)

One `provider.ChatCompletion` call with:

- `System: flowdoc.Brief` (schema + ref policy)
- `UserPrompt: tree + curated files + emission instruction`
- `JSONMode: true`, `Temperature: 0.0`
- `MaxTokens: defaultFlowdocOutputTokens` (16384, overridable)

Default model is `grok-4-fast` — cheap, fast, large-context, no tool-use
required. Operator can override with `--model`.

### Agentic path (`runFlowdocAgenticLLM`)

`provider.RunTools` drives a multi-turn loop. The model is seeded with
the orienting prompt (`Brief + BriefAgenticAddendum` system, a
"discover the project" user message) and a four-tool catalogue:

| Tool          | Backend                                          | Purpose                                                      |
| ------------- | ------------------------------------------------ | ------------------------------------------------------------ |
| `read_file`   | `RepoView.ReadFile`                              | Read one kept file (1 MiB cap, binary-refused).              |
| `grep`        | `RepoView.Search`                                | RE2 search across kept files, up to `grepHitCap = 50` hits.  |
| `list_dir`    | derived from `RepoView.Files`                    | Direct or recursive listing.                                 |
| `check_ref`   | shared `verifyStepRef` (same code path as `verify`) | Resolve one `step.ref` in-loop before final emission.       |

`check_ref` is the cost-reduction lever: the model lints its own refs
mid-loop, paying one tool call per ref rather than risking a full
emission that `vv flowdoc verify` would reject. The brief's "Ref
hygiene (REQUIRED)" section instructs the model to call it on every ref
before emitting.

Default agentic model is `claude-haiku-4-5` (cheapest tool-use-capable
Anthropic model); `--model` accepts any Anthropic model. The loop is
bounded by `--max-iterations` (default 30; the pre-retirement
wrap-dispatch default of 10 was undersized for repo exploration).

A `max_tokens` stop reason emits a warning suggesting the operator
raise `--max-iterations`.

### Output and failure modes

`finalizeFlowdoc` tolerates two common LLM-output quirks:

- **Markdown fences** — `stripJSONFence` removes a leading
  ` ```json` / ` ``` ` and trailing ` ``` `.
- **Leading prose** — the model occasionally precedes the JSON with
  `"Now I will produce flows.json:"`. `finalizeFlowdoc` advances to the
  first `{` and decodes with a streaming `json.Decoder` so trailing
  commentary is ignored.

On parse failure the **raw response is preserved** at
`<root>/doc/flows.json.broken` for inspection — without this the
operator loses the entire run. A response that does not end with `}`
triggers an additional "likely truncated, raise `--max-output-tokens`"
hint.

After parse, missing metadata fields (`SchemaVersion`, `Project`,
`Generator`, `GeneratedAt`) are stamped from defaults so the model can
omit them. `Validate` runs before any file write, so the on-disk
artifact is always schema-conformant.

JSON is written with two-space indent + trailing newline (POSIX-friendly
diffs). HTML is written via `flowdoc.Render` using the embedded
`viewer.html` template.

## `vv flowdoc verify` — zero-LLM lint

`runFlowdocVerify` reads `<root>/doc/flows.json`, runs `Validate`
(structural) first, then `VerifyRefs` (drift detection).

`VerifyRefs` walks every `nodes[].path` and every `flows[].steps[].ref`,
classifying issues:

| `RefIssueKind`         | Severity | Trigger                                                                    |
| ---------------------- | -------- | -------------------------------------------------------------------------- |
| `missing-file`         | error    | Node path or ref path doesn't exist on disk.                               |
| `missing-symbol`       | error    | `path:Symbol` ref where `path` exists but `Symbol` isn't declared in it.   |
| `out-of-range-line`    | error    | `path:N` ref where `N` is past EOF.                                        |
| `dangling-node-ref`    | error    | (caught by `Validate`; included here for symmetry.)                        |
| `weak-match`           | warning  | `path:N` ref where line exists but isn't a recognized declaration line.    |

### Ref parsing (`parseRef`)

Refs come in three forms:

- `path` — bare path.
- `path:123` — `path:line`, suffix is all ASCII digits.
- `path:Symbol` — `path:symbol`, non-empty non-numeric token.

Splitting uses `lastSingleColonIndex` — the last `:` not adjacent to
another `:` — so `dir/file.cpp:Type::method` splits into
`dir/file.cpp` and `Type::method`, not inside the C++/Rust scope
qualifier.

Package-form `path:Symbol` refs (where `path` is a package directory)
resolve via a depth-4 recursive grep with build-output / dependency
directories pruned (`vendor`, `target`, `node_modules`, `dist`,
`build`, etc.). Handles Rust crate `crate/src/...` and C/C++
`proj/src/sub/...` layouts naturally.

### Multi-language symbol grammar (`symbolDeclared`)

Dispatches by file extension/basename via `detectLangByPath` to a
language-specific declaration-pattern set:

- **Go** — `func`, method, `type`, `[const|var]`.
- **Rust** — `fn`, `struct`, `enum`, `trait`, `impl`, `mod`,
  `const`/`static`.
- **C/C++** — function definitions, `struct`/`class`/`union`/`enum`,
  `typedef`, top-level variable defs, preprocessor `#define`.
- **CMake** — `add_executable`, `add_library`, `add_custom_target`,
  `function()`/`macro()`.
- **Make** — bare-target rule (`target:`).
- **Python** — `def`, `class`.

Unknown extensions **auto-accept the symbol** (returns true) — no
false-positive `missing-symbol` on unfamiliar file types. Extending is
one entry in `declPatterns`.

### Lenient fallbacks

Three weak-match fallbacks reduce noise on common model drift modes:

1. Bare-path ref where parent dir exists (case-mismatch on basename).
2. `path:Symbol` ref where the parent dir contains a file declaring
   `Symbol` even if the named path is off.
3. `path:Symbol` ref where the named file exists and a sibling file
   declares `Symbol`.

Each downgrades a hard error to a `weak-match` warning. The doc is
technically drifted but resolves intent without blocking the lint.

### Exit codes

| Outcome                      | Exit |
| ---------------------------- | ---- |
| Clean (zero issues)          | `0`  |
| Warnings only (weak-match)   | `0`  |
| Any hard error               | `1`  |
| Missing `doc/flows.json`     | `1`  |
| Parse / validate failure     | `1`  |
| Bad flag                     | `2`  |

## `vv check` integration — `flowdoc` row

When `doc/flows.json` exists in the project, `vv check` adds a `flowdoc`
row that re-runs `VerifyRefs` and reports the result:

| `vv check` status | Meaning                                                  |
| ----------------- | -------------------------------------------------------- |
| `pass`            | Clean — `N flows, M nodes, no drift`                     |
| `warn`            | Weak-match warnings only.                                |
| `fail`            | One or more hard errors.                                 |

Projects without `doc/flows.json` skip the row silently — flowdoc is
opt-in per project.

## HTML viewer

`viewer.go` + `viewer.html` ship as `go:embed`-ed assets. `Render(w,
doc)` executes the template against the `FlowDoc`. The viewer is a
~300–500 LoC vanilla-JS frontend — no build step, no external CDN —
that renders the flow graph interactively. The operator opens
`doc/FLOWS.html` directly in a browser (or via `vv flowdoc gen --open`,
which fires `xdg-open` on Linux or `open` on macOS, detached).

## Dry-run / inspection

`vv flowdoc gen --dry-run` (alias `--show-context`) is the
diagnostic-without-LLM-spend handle:

- **Single-shot dry-run** — prints project metadata, walk source, file
  enumeration counts, key-file selection (`+` included, `-` dropped),
  byte budget, and the full system + user prompts.
- **Agentic dry-run** (`--strategy agentic --dry-run`) — prints the
  metadata, tool catalogue, model/iteration cap, and the system +
  initial-user prompts. The multi-turn exploration is opaque without
  a real model run, but the setup is fully inspectable.

This is the workflow for cost estimation (how big is my context?) and
brief tuning (does the prompt say what I think it says?) without
spending tokens.

## Defaults summary

| Knob                          | Default                    | Override                |
| ----------------------------- | -------------------------- | ----------------------- |
| Default model                 | `grok-4-fast`              | `--model <id>`          |
| Default agentic model         | `claude-haiku-4-5`         | `--model <id>`          |
| Strategy                      | `auto`                     | `--strategy <s>`        |
| Output `MaxTokens`            | `16384`                    | `--max-output-tokens N` |
| Context byte budget           | `DefaultContextBudgetBytes` (~256 KiB) | `--max-context-bytes N` |
| Agentic max iterations        | `30`                       | `--max-iterations N`    |
| LLM call timeout              | `10 * time.Minute`         | (hardcoded)             |
| Single-file read cap          | `1 << 20` (1 MiB)          | (hardcoded)             |
| Grep hit cap per call         | `50`                       | (hardcoded)             |

## Git reference range

The flowdoc epic shipped as a single feature branch aggregate-merged to
main on 2026-05-16 (iter 238 ship, iter 239 reconciliation). The
canonical range — 13 commits, +13086/-18 LoC across 63 files — is:

```
1428fb1..f2d6545
```

Browse as a unified diff on GitHub:

[**github.com/suykerbuyk/vibe-vault — compare `1428fb1...f2d6545`**](https://github.com/suykerbuyk/vibe-vault/compare/1428fb1...f2d6545)

Commit-by-commit (chronological):

| SHA       | Subject                                                                                  |
| --------- | ---------------------------------------------------------------------------------------- |
| `b900521` | feat(flowdoc): translate spike artifact into golden testdata fixture (Phase 1)           |
| `e42f7fd` | feat(llm): plumb MaxTokens through Request and all providers (Phase 2)                   |
| `9ace416` | feat(flowdoc): add schema types and Validate (Phase 3)                                   |
| `f6af684` | feat(flowdoc): add embedded HTML viewer template and Render helper (Phase 5)             |
| `f1f3a3b` | feat(cmd,flowdoc): add vv flowdoc gen subcommand and generator brief (Phase 4)           |
| `165b9a7` | feat(flowdoc): add VerifyRefs ref-checker and vv flowdoc verify verb (Phases 1-2)        |
| `975a6dd` | test(flowdoc): add golden-fixture integration test, consolidate golden_test (Phase 6)    |
| `0da754e` | feat(check): add flowdoc staleness check row (Phase 3)                                   |
| `a252db1` | feat(flowdoc,llm,cmd): restore agentic generator + repo introspection (Phase 4a-c)       |
| `2c75264` | feat(flowdoc): multi-language verifier grammar + lenient fallbacks + Brief ref-policy    |
| `b451cec` | doc(vibe-vault): ship 27-flow flows.json + FLOWS.html + measurement script               |
| `d212905` | feat(flowdoc): check_ref agentic tool + Brief mandate                                    |
| `f2d6545` | chore(wrap): stamp iter 238 — flowdoc-epic completed                                     |

The endpoint `f2d6545` is the iter-238 wrap-stamp commit closing the
epic; the last functional change is `d212905`. For a code-only diff
excluding the wrap stamp, use `1428fb1...d212905`.

## Completeness-audit verdict (iter 239)

A four-subagent A/B audit (markdown chain vs. `flows.json`, with a
recmeet cross-language datapoint and a blind evaluator, all Sonnet 4.6)
classified `flows.json` as **augmentation-only** — keep it as an
operator-facing navigation index, but do **not** wire it into the
`/restart` chain as a markdown replacement. The load-bearing finding:
edge-only encoding cannot surface architecturally important seams that
are not call edges (DESIGN #103 two-tier staging was invisible to the
model reading the JSON because `internal/staging` is a Storage node but
not a step in the session-capture-hook flow's `steps[]`). Re-running
the audit is justified only if the schema grows a `seams:` field for
non-call-edge boundaries (carry-forward
`flowdoc-schema-seams-field-rerun-audit`).
