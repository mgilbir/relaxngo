# RelaxNGo — Adversarial Codebase Audit

**Date:** 2026-07-12
**Scope:** Full read of every Go source file (~24.8k LOC), all docs, scripts, config, and tooling.
**Method:** Per-area expectation-vs-reality tracing; every finding is marked **CONFIRMED** (traced end-to-end and/or reproduced at runtime) or **PLAUSIBLE** (strongly suspected from code, not reproduced). Runtime repros were built against the repo via a `replace`-directive scratch module and `go test -race`.

**Stable IDs:** findings keep their package-prefixed IDs so a fixing agent can cite them unambiguously — `R#` = `rng`, `V#` = `validator`, `G#` = `generator`/tooling, `X#` = cross-cutting/whole-repo. Section 1 is the severity-ordered master view.

---

## 1. Summary table (severity order)

| ID | Sev | Area | Issue (one line) | Location | Status |
|----|-----|------|------------------|----------|--------|
| V1 | Critical | validator | Attributes of non-root elements are never validated (default config) | validator/pattern.go:1401 | CONFIRMED |
| V2 | Critical | validator | Schema sequence order discarded; `optional`/`zeroOrMore` before a sibling breaks | validator/pattern.go:434 | CONFIRMED |
| V3 | Critical | validator | A `<choice>` sibling of element children is silently dropped from content model | validator/pattern.go:439 | CONFIRMED |
| V4 | Critical | validator | Concurrent `Validate` on a `pattern`-facet schema data-races on global `regexCache` (panic) | validator/validator.go:2615 | CONFIRMED |
| R1 | High | rng | Absolute `href` bypasses base-dir containment → arbitrary local file read | rng/parser.go:51 | CONFIRMED |
| X1 | High | parser | `StrictParseXML` infinite-loops (hang) on recursive struct types | parser/strict.go:185 | CONFIRMED |
| G1 | High | generator | Directly-nested complex elements → non-compiling generated code (`undefined: B`) | generator/types.go:255 | CONFIRMED |
| G2 | High | generator | `choice` / element-level `interleave` silently dropped from structs → data loss | generator/types.go:37 | CONFIRMED |
| G3 | High | generator | `ref` fields use define name for type+tag → non-compiling / wrong tags | generator/types.go:471 | CONFIRMED |
| G5 | High | generator | Benchmark project names derived from XML *content* → every generated bench is broken | cmd/internal/generate-benchmark-projects/main.go:246 | CONFIRMED |
| V5 | High | validator | Interleave semantics violate RELAX NG §9; two engines disagree | validator/pattern.go:2540 | CONFIRMED |
| V6 | High | validator | `TokenBuffer` corruption when interleave fails inside a choice → valid docs rejected | validator/pattern.go:2466 | CONFIRMED |
| V7 | High | validator | `Line`/`Column` report decoder read-ahead position, not error site | validator/validator.go:74 | CONFIRMED |
| V8 | High | validator | Namespaces not checked below root; `ns=` ignored | validator/pattern.go:2188 | CONFIRMED |
| V9 | High | validator | "Full XSD type validation" false — most datatypes validate nothing | validator/validator.go:2477 | CONFIRMED |
| V10 | High | validator | XSD `pattern` facet matched unanchored (substring) | validator/validator.go:2597 | CONFIRMED |
| R2 | High | rng | Diamond/DAG include falsely rejected as a cycle | rng/parser.go:7224 | CONFIRMED |
| X2 | High | docs | README Quick Start & strict-mode examples don't compile (`rng.ParseFile`, `parser.NewStrictParser`) | README.md:27,163 | CONFIRMED |
| X3 | High | docs | README output examples are fiction (codegen output, "4.9M fuzz execs", "136/136 tests") | README.md | CONFIRMED |
| G6 | Med-High | generator | Generated globals + `formatValidationErrors` collide; two schemas can't share a package | generator/unmarshaler.go:160 | CONFIRMED |
| R4 | Med-High | rng | Serializer emits stale `Group.RawContent`, dropping resolved includes/externalRefs | rng/serializer.go:510 | CONFIRMED |
| X4 | Med-High | parser | `StrictParseXML` false-positives on `xml:"a>b"` tags; ignores namespaces | parser/strict.go:104 | CONFIRMED |
| V11 | Medium | validator | Malformed XML mid-doc / trailing junk not reported; legacy paths swallow decoder errors | validator/validator.go:246 | CONFIRMED |
| V12 | Medium | validator | Empty / root-less documents validate as valid | validator/validator.go:234 | CONFIRMED |
| V13 | Medium | validator | Options largely decorative (`MaxInterleave`/`CollectUnknown` dead; `MaxDepth`/`FailFast`/`MaxErrors` weak) | validator/validator.go:36 | CONFIRMED |
| V14 | Medium | validator | No size limit / no cancellation in validation; whole subtree buffered | validator/pattern.go:1774 | CONFIRMED |
| V15 | Medium | validator | `official_suite.go` (1203 lines) shipped in importable package; lenient pass criteria | validator/official_suite.go:475 | CONFIRMED |
| V16 | Medium | validator | Permissive fallbacks accept anything for unrecognized content | validator/pattern.go:661 | CONFIRMED |
| G4 | Medium | generator | Two elements same name, different content → second silently discarded | generator/types.go:193 | CONFIRMED |
| G7 | Medium | generator | `panic` in `sync.Once` in consumer code; after panic validation silently disabled forever | generator/unmarshaler.go:35 | CONFIRMED |
| G8 | Medium | generator | Code generation nondeterministic (map iteration) — output shuffles between runs | generator/types.go:206 | CONFIRMED |
| G10 | Medium | generator | Official-suite roundtrip test is a tautology (marshals object against itself) | generator/official_suite_test.go:295 | CONFIRMED |
| G11 | Medium | generator | Tests written to mask interleave/choice field loss | generator/generation_test.go:118 | CONFIRMED |
| G15 | Medium | generator | `getAttributeFieldType` types an attribute from the element's `<list>` → runtime unmarshal error | generator/types.go:406 | PLAUSIBLE |
| G16 | Medium | tooling | `scripts/run_fuzzing.sh` non-functional; fuzz targets don't exist | scripts/run_fuzzing.sh:17 | CONFIRMED |
| G17 | Medium | tooling | `scripts/profile.sh` uses profile flags with multi-package `./...` → fatal | scripts/profile.sh:17 | CONFIRMED |
| R3 | Medium | rng | Undefined refs inside container patterns not validated | rng/parser.go:2374 | CONFIRMED |
| R6 | Medium | rng | "50MB limit" absent for schema/include parsing (unbounded) | rng/parser.go:58 | CONFIRMED |
| R7 | Medium | rng | Indirect nullable recursion not detected | rng/parser.go:2061 | CONFIRMED |
| X5 | Medium | DX/CI | `make lint` reports 83 issues though README requires "linter passes"; no CI at all | .golangci.yml | CONFIRMED |
| X6 | Medium | validator | `CachedValidator` name lies — caches nothing but the grammar, rebuilds patterns every call | validator/cache.go:13 | CONFIRMED |
| V17 | Medium | validator | `readInnerXML` re-serializes schema fragments without escaping/namespaces | validator/pattern.go:1747 | CONFIRMED (code); PLAUSIBLE (impact) |
| V18 | Low-Med | validator | `matchChoicePat` "consume everything unless last alt" makes choice context-dependent | validator/pattern.go:1952 | CONFIRMED |
| V23 | Low-Med | validator | `ValidationError` fields inconsistently populated; `Element` sometimes gets a path | validator/validator.go:449 | CONFIRMED |
| R11 | Low | rng | Serializer round-trip lossy (type/datatypeLibrary/name-class) | rng/serializer.go:886 | CONFIRMED |
| R5/G13/X2 | Med | docs | `rng.ParseFile` in README doesn't exist | README.md:27 | CONFIRMED |
| R8 | Low | rng | Nullable-recursion check skips `OneOrMore` at define level; contradictory error msg | rng/parser.go:2073 | CONFIRMED |
| R9 | Low | rng | `strings.Contains(path,"..")` over-broad; rejects legit `a..b.rng` | rng/parser.go:46 | CONFIRMED |
| R10 | Low | rng | `decodeExternalRefAsGrammar` wraps a nil error (`%!w(<nil>)`) | rng/parser.go:7086 | CONFIRMED |
| R12 | Low | rng | `ParseSchema` silently accepts only `<grammar>` root (undocumented) | rng/parser.go:709 | CONFIRMED |
| R13 | Low | rng | Round-trip test compares only define counts/names — can't catch serializer bugs | rng/roundtrip_test.go:175 | CONFIRMED |
| R14 | Low | rng | Nested-grammar xmlns injection misses `<grammar attr…>` | rng/parser.go:5777 | PLAUSIBLE |
| R15 | Low | rng | ExternalRef-cycle comment says "allowed" but code errors | rng/parser.go:7516 | CONFIRMED |
| V19 | Low | validator | `regexCache` capped at 100 then recompiles every call (also part of V4 race) | validator/validator.go:2628 | CONFIRMED |
| V20 | Low | validator | `countListDataPatterns` parses schema with `strings.Index` | validator/validator.go:2380 | CONFIRMED |
| V21 | Low | validator | Duplicated, divergent logic between the two engines | validator/validator.go | CONFIRMED |
| V22 | Low | validator | Dead code (`AttributePat`, `AnyName`/`NsName`, stubs) | validator/pattern.go:188 | CONFIRMED |
| V24 | Low | validator | `oneOrMoreChoiceAttributeMatched` single context-global flag | validator/validator.go:263 | CONFIRMED |
| V26 | Low | validator | No `CharsetReader` — non-UTF-8 docs fail (XXE not possible: positive) | validator/validator.go:216 | CONFIRMED |
| V27/G10/G11/R13 | Low | tests | Test suite shaped so it cannot catch its own bugs | multiple | CONFIRMED |
| V28 | Low | validator | Legacy path (`UsePatternAST=false`) structural bugs | validator/validator.go:1725 | CONFIRMED |
| G9 | Low-Med | repo | `dummy/` is stale manual generator output (`package XXX`), silently built | dummy/simple.go:1 | CONFIRMED |
| G12 | Low | generator | Test helper duplicates `sanitizeIdentifier` without keyword handling | generator/helpers_test.go:10 | CONFIRMED |
| G14 | Low-Med | generator | `GenerateCode`'s `schemaContent` param dead; doc contradicts impl | generator/types.go:786 | CONFIRMED |
| G18 | Low | tooling | `cmd/internal/extract-test` dead: hardcoded path to non-existent file | cmd/internal/extract-test/extract.go:51 | CONFIRMED |
| G19 | Low | tooling | Stale path references in tool error hints / doc comments; 3rd copy of type-name logic | multiple | CONFIRMED |
| G20 | Low | generator | Minor generated-code quality (ignored errcheck, debug leftover, whitespace) | generator/unmarshaler.go | CONFIRMED/PLAUSIBLE |

**Counts:** Critical 4 · High 15 · Medium ~20 · Low ~25. (~64 findings.)

---

## 2. System map

### Packages
| Package | Role | Public entry points |
|---------|------|---------------------|
| `rng` | Parse & serialize RELAX NG schemas | `ParseSchema(io.Reader)`, `ParseSchemaFile(path, baseDir)`, `ParseSchemaWithResolver`, `SerializeGrammar`, `ResourceResolver`/`DiskResolver` |
| `validator` | Validate XML against a `*rng.Grammar` | `NewValidator`, `DefaultOptions`, `Validator.Validate(io.Reader)`, `CachedValidator`, plus `official_suite.go` harness (mis-shipped) |
| `generator` | Emit Go structs + `Validate`/`UnmarshalXML` from a grammar | `GenerateTypes`, `GenerateCode` |
| `parser` | Unrelated struct-based XML helpers | `ParseXML`, `StrictParseXML`, `StrictParseXMLWithLimit`, `ValidateXML` |
| `cmd/generate` | CLI: schema → Go (`-schema`, `-package`, stdout) | correct |
| `cmd/internal/*` | Test-suite / benchmark / extract tooling | several broken (G5, G16–G19) |

### Real execution paths
- **Validate (default):** `Validate` → `LineTracker`+`xml.Decoder` → first `StartElement` → `validateElement` (root only: name + attributes) → `validateContentWithAST` → `BuildPatternFromElement` (partly from `rng.*` structs, partly by re-serializing/re-parsing `RawContent`) → buffer **entire** remaining document into a `TokenBuffer` → `MatchPattern` (recursive backtracking matcher). A second, divergent legacy streaming engine exists behind `UsePatternAST=false`.
- **Schema parse (with includes):** `ParseSchemaWithResolver` → decode → flatten divs → validate → normalize/resolve QNames → propagate `datatypeLibrary` → resolve includes/externalRefs via `ResourceResolver` (shared, never-cleared `visited` map) → merge combines → synthesize implicit patterns.
- **Generate:** `cmd/generate` → `ParseSchemaFile` → `GenerateTypes` (field passes per `rng` sub-field) → `GenerateCodeWithUnmarshal` (embeds `SerializeGrammar(grammar)`, emits per-root `Validate`/`UnmarshalXML`) → `format.Source` → stdout.

### Key invariants (many only *assumed*, not enforced)
- **Assumed:** define name == element name (broken → G3); every content sub-pattern is order-independent (broken → V2/V3); attributes only appear on the root (broken → V1); documents fit in memory (V14); one generated file per package (G6); grammar reachable concurrently is immutable (broken by `regexCache` → V4).
- **Enforced & sound:** include cycle *detection* fires (but over-eager, R2); relative `../` traversal blocked (but absolute bypasses, R1); no XXE (Go stdlib never resolves external entities — verified); `rng` parsing has no mutable global state and is race-clean under `-race`.

---

## 3. Findings by category (severity order)

> Detailed per-package findings for `rng` (R#) and `validator`/`generator` (V#/G#) were produced by dedicated deep reads and are reproduced verbatim in the appendix files below. Only the cross-cutting/whole-repo findings (X#) and the top criticals are expanded here; every ID above is real and independently spot-checked where marked CONFIRMED.

### Critical

**V1 — Non-root attributes are never validated (default configuration). CONFIRMED (runtime).**
`buildPattern`'s content builder discards attribute patterns (`pattern.go:1401`: `case "attribute": _ = skipToEnd(decoder) // attributes are not supported in patterns`), and `matchElementPat` never inspects `start.Attr`. `validateAttributes` runs only for the root element.
Repro: schema requiring `<name lang="…">` with no extra attributes. Documents with the attribute **missing**, with a **wrong** value, and with an **extra unknown** attribute all return **0 errors**. Only the unit tests pass because all their attributes sit on the root.
Direction: validate attributes inside `matchElementPat` for every element, not just the root.

**V2 — Schema sequence order discarded for element content. CONFIRMED (runtime).**
`buildGroupLikePattern` (pattern.go:434) assembles content by *field type* (Elements, then Group, Optional, OneOrMore, ZeroOrMore), not document order.
Repro (this audit re-ran it): schema `(optional o, element m)` rejects the valid `<o/><m/>` ("expected element 'm', got 'o'") and — mirror image — a `(zeroOrMore b, element a)` schema accepts the invalid `<a/><b/>` while rejecting the valid `<b/><a/>`.
Direction: build the content model in schema document order (route element children through the ordered `RawContent` path uniformly, or record positions).

**V3 — Sibling `<choice>` dropped from content model. CONFIRMED (runtime).**
When an element has direct element children, `buildGroupLikePattern` never consults `elem.Choice`. Schema `(choice(a,b), c)` rejects valid `<a/><c/>` / `<b/><c/>` and accepts the invalid `<c/>` alone.

**V4 — Concurrent `Validate` on a `pattern`-facet schema races on the global `regexCache`. CONFIRMED (runtime, `-race`).**
`var regexCache = make(map[string]*regexp.Regexp)` (validator.go:2615) is read/written with no lock. Concurrent map access is a Go runtime panic. Repro: 200 goroutines validating a `<data><param name="pattern">` schema on one `Validator` → repeated `WARNING: DATA RACE`. The bundled concurrency test only passes because its schema has no `pattern` facet. Directly falsifies README "Concurrent validation ✅" and `CachedValidator` "Thread-safe."
Direction: `sync.Map` or a per-`Validator` cache with a mutex.

### High (cross-cutting)

**X1 — `parser.StrictParseXML` hangs forever on recursive struct types. CONFIRMED (runtime).**
`extractKnownFieldsRecursive` (parser/strict.go:185) recurses into struct-typed fields with no visited-set; a self-referential type (`type Node struct{ Children []Node }`) recurses until the process is killed (reproduced: 20s hard timeout on a two-level document).
Direction: track visited types during reflection walk.

**X2 — README's headline code examples don't compile. CONFIRMED.**
`rng.ParseFile("schema.rng")` (README:27,127) — no such function (real: `ParseSchema`/`ParseSchemaFile`). The Quick Start also references an undeclared `file` variable. The entire "Detect Unknown Fields (Strict Mode)" example (README:163) calls `parser.NewStrictParser()` and `p.ParseXML` — neither exists; the architecture table's `StrictParser` "main type" (README:180) is fictional. `go vet` on the verbatim snippet: `undefined: rng.ParseFile`.

**X3 — Multiple README claims are fabricated. CONFIRMED.**
- The codegen "Example output" (clean `Book`/`Library` structs, single `encoding/xml` import) is nothing like real output — the generator emits an 8-import file embedding the schema as a const plus exported `*Validator`/`*Once` globals and `Validate`/`UnmarshalXML` methods (see `dummy/simple.go`, which is exactly this output).
- "Fuzz Tests: 4.9M+ executions, 0 crashes ✅" — there are **zero** `func Fuzz` targets in the repo.
- "Unit Tests: 136/136 passing" — the repo has 78 test functions / ~854 subtest executions; 136 matches nothing.
- README perf table ("240k–300k docs/sec") contradicts `BENCHMARK_RESULTS.md`, and the throughput is inflated precisely because most checks (V1, V9) don't run.

**X4 — `StrictParseXML` false-positives on `xml:"a>b"` and ignores namespaces. CONFIRMED (runtime).**
The reflection walker keys fields by local name in a flat path and doesn't understand Go's `parent>child` tag nesting: a struct with `xml:"meta>title"` validated against `<doc><meta><title/></meta></doc>` reports `unknown elements: doc.meta`. Separately, an element in a *different* namespace with the same local name is accepted as known (namespace never compared).

### High/Medium (per-package — see appendix)
R1, R2, R3, R4, R6, R7 (rng); V5–V17 (validator); G1–G20 (generator/tooling). Each carries a concrete repro in its appendix entry; the top ones (R1 absolute-path read, R2 diamond-include false cycle, G1/G3 non-compiling output, G2 silent data loss, G5 dead benchmark harness) were independently spot-checked and confirmed during this audit.

**X5 — `make lint` fails with 83 issues; no CI exists. CONFIRMED.**
README requires "Linter passes: `make lint`" for contributions, but `golangci-lint run` reports 83 issues (gosec 6, errcheck 3, govet 2, staticcheck 10, goconst 49, prealloc 8, revive 4, funlen 1). There is no `.github/` — nothing enforces build/test/lint on push. A newcomer following CONTRIBUTING cannot land a green lint.

**X6 — `CachedValidator` caches nothing. CONFIRMED.**
Despite the name and "efficient validation across multiple documents" docstring (cache.go:13), it stores only the parsed grammar (which a plain `Validator` already holds) behind a mutex; every `Validate` still rebuilds the whole pattern AST and re-parses `RawContent` from scratch. It is a near-duplicate of `Validator` plus `UpdateOptions`. The name promises memoization the code doesn't deliver.

---

## 4. Design tensions (structural, not line-level)

**D1 — Two divergent validation engines, neither derivative-based.**
The validator ships an AST-buffer-backtracking engine *and* a legacy streaming tree-walker with mutually inconsistent semantics (V5, V13, V21). RELAX NG has a well-known, correct, ~200-line implementation strategy: **Brzozowski/Clark derivatives** (`nullable`/`deriv`), which is streaming, O(1)-memory, order-correct, and handles interleave/choice/attributes uniformly. The current code reimplements a worse version of this twice and gets ordering (V2/V3), interleave (V5/V6), and attributes (V1) wrong. *Alternative to weigh:* replace both engines with a single derivative-based matcher; delete `pattern.go`'s backtracking buffer and the legacy path entirely. This collapses V1–V6, V16, V18, V21, V22, V24, V28 into one rewrite.

**D2 — `RawContent []byte` as a parallel source of truth.**
Nearly every `rng` pattern struct carries `,innerxml` raw bytes that are re-serialized and re-parsed downstream (validator `readInnerXML`, serializer `serializeGroupContent`). This duplicated representation drifts from the structured fields after resolution, producing R4 (serializer drops resolved includes), V17 (unescaped re-serialization), and the whole class of "parse XML with `strings.Index`" hacks (V20, V15's DOCTYPE surgery). *Alternative:* commit to a single fully-structured AST after simplification; never keep raw bytes past parse.

**D3 — Generator assumes a degenerate schema shape.**
The generator is correct only when: one define per element, define name == element name, no `choice`, no element-level `interleave`, complex content only behind `optional`/`group`/`oneOrMore`/`zeroOrMore`. Outside that envelope it emits non-compiling code (G1, G3) or silently loses data (G2, G4). RELAX NG's `choice`/`interleave`/co-constraints don't map cleanly onto Go structs at all — `encoding/xml` can't express "exactly one of a|b." *Alternative:* either restrict the generator to the expressible subset and *error loudly* on the rest, or generate custom `UnmarshalXML` logic (not plain struct tags) for choice/interleave. Today it silently under-generates.

**D4 — Test suites engineered to stay green.**
"100% official suite," "136/136," and "roundtrip verified" coexist with dozens of confirmed misvalidations because the harness counts an Invalid-doc test as passed on *any* error (V15), the roundtrip test marshals an object against itself (G10), and unit tests `t.Logf` instead of asserting (V27, G11, R13). The suite measures "doesn't crash," not "is correct." *Alternative:* assert exact expected errors; re-parse in roundtrip tests; make Invalid-doc tests assert the *specific* rejection reason.

**D5 — Package boundaries leak test/tooling code into the shipped API.**
`validator/official_suite.go` (1203 lines, imports `html`/`os`) is compiled into every importer (V15); `dummy/` (G9) adds a `package XXX` with exported `Person*` globals to `go build ./...`; the generator's output exports mutable package globals into the consumer's API (G6). *Alternative:* move `official_suite.go` to `internal/` or `_test.go`; delete/`.gitignore` `dummy/`; make generated validators use unexported, per-file symbols.

---

## 5. Expectation gaps (expected X, found Y)

| Affordance / doc | Expected | Found |
|---|---|---|
| README Quick Start | `rng.ParseFile` compiles and validates | `ParseFile` doesn't exist; snippet won't build (X2) |
| README strict-mode | `parser.NewStrictParser().ParseXML` | neither symbol exists (X2) |
| Codegen "Example output" | plain `Book`/`Library` structs | 8-import self-validating file w/ exported globals (X3) |
| `ValidationError.Line/Column` "precise" | error's line/column | decoder read-ahead position, usually EOF (V7) |
| "Full XSD type validation" | `date`, `byte` range, `decimal` checked | most types `return true`; no range checks (V9) |
| "Concurrent validation ✅" / "Thread-safe" | safe under goroutines | data race + panic on pattern schemas (V4) |
| "Path Traversal Protection" | includes confined to base dir | absolute paths escape; `..` check over-broad (R1, R9) |
| "DoS Prevention: 50MB limit on documents" | validation bounded | limit only in unrelated `StrictParseXML`; validation unbounded (V14, R6) |
| "Cardinality/Optional/Types" codegen | int64/bool/float64 mapping | required child elements stay `string`; ns dropped (G13) |
| Options struct | 6 knobs configure behavior | 2 dead, 3 weak, 1 honored (V13) |
| `make lint` (CONTRIBUTING) | passes | 83 issues (X5) |
| `CachedValidator` | memoizes for reuse | caches only the grammar; rebuilds every call (X6) |
| Official suite "100%" | spec-conformant | lenient harness; misvalidates ordinary schemas (V1–V3, V15) |

## 6. Open questions (code alone can't resolve)

1. **Intended engine.** Is the legacy streaming engine (`UsePatternAST=false`) deprecated, or a supported fallback? If deprecated, deleting it removes ~800 lines and half of V21/V28.
2. **Generator contract.** Is the generator meant to support arbitrary RELAX NG, or a documented subset? That decision determines whether G1–G4 are bugs or missing "unsupported" errors.
3. **`official_suite.go` placement.** Was shipping it in the importable package deliberate (V15)? It looks accidental (only consumer is a `cmd/internal` tool).
4. **`dummy/` provenance.** It's untracked `package XXX` output — safe to delete, or a fixture someone relies on locally (G9)?
5. **Absolute-include policy.** Should absolute `href`s be allowed at all (R1)? If the security claim is load-bearing, they must be rejected or confined via `os.Root`.
6. **Compliance-number source.** Where did "136/136" and "4.9M fuzz executions" come from (X3)? They match no current artifact — a prior branch, or aspirational copy?

---

## Appendix A — `rng` package findings (R1–R15)

*(Full detail as traced; each CONFIRMED item was reproduced or line-traced.)*

- **R1** (High, CONFIRMED) rng/parser.go:51 — `DiskResolver.ReadResource` uses an absolute `href` verbatim, bypassing `BaseDir`; `<include href="/etc/passwd"/>` reads outside the base dir. Contradicts "Path Traversal Protection."
- **R2** (High, CONFIRMED) rng/parser.go:7224 — `visited` map is created once and never cleared ("Do NOT use defer to delete"); a leaf included from two branches of a DAG trips "include cycle detected." Contradicts the re-inclusion merge design.
- **R3** (Medium, CONFIRMED) rng/parser.go:2374 — `validateElementNestedRefs` recurses into child elements but never checks container ref lists (`choice.Refs`, `oneOrMore.Ref`, …); an undefined `<ref>` inside `<oneOrMore>` parses with no error.
- **R4** (Med-High, CONFIRMED) rng/serializer.go:510 — `serializeGroupContent` dumps stale `group.RawContent` and returns, ignoring structured fields; a group containing a resolved `<externalRef>` serializes back to the unresolved `<externalRef href=…>` and loses the resolved element. Contradicts `SerializeGrammar`'s "all includes expanded" docstring.
- **R5** (Medium, CONFIRMED) README `rng.ParseFile` doesn't exist (merged into X2).
- **R6** (Medium, CONFIRMED) rng/parser.go:58 — schema/include reads are unbounded `os.ReadFile`; the 50MB limit lives only in `parser/strict.go`.
- **R7** (Medium, CONFIRMED trace) rng/parser.go:2061 — recursion check only compares `ref.Name == defineName`; indirect nullable loops (`x→y?`, `y→x?`) undetected.
- **R8** (Low, CONFIRMED) rng/parser.go:2073 — `checkDefineForRecursiveNullableRefs` omits `def.OneOrMore`; the `OneOrMore` branch is unreachable at define level and its error message says "(not nullable)" while erroring.
- **R9** (Low, CONFIRMED) rng/parser.go:46 — `strings.Contains(cleanPath,"..")` rejects legit `..config`, `a..b.rng`.
- **R10** (Low, CONFIRMED) rng/parser.go:7086 — nil-error wrap yields `%!w(<nil>)` when decode succeeds but `Href==""`.
- **R11** (Low, CONFIRMED) rng/serializer.go:886 — always writes `type=""`, drops `DatatypeLibrary` and name-class names; round-trip lossy.
- **R12** (Low, CONFIRMED) rng/parser.go:709 — `ParseSchema` decodes only into `Grammar`; simplified-syntax bare-pattern roots silently fail; undocumented.
- **R13** (Low, CONFIRMED) rng/roundtrip_test.go:175 — `grammarsAreEquivalent` compares only define counts/names + "start has content"; can't catch R4.
- **R14** (Low, PLAUSIBLE) rng/parser.go:5777 — nested-grammar xmlns injection matches literal `<grammar>` only; `<grammar attr…>` misses the injection and fails to unmarshal.
- **R15** (Low, CONFIRMED) rng/parser.go:7516 — comment says externalRef cycles are allowed/broken-in-place; code immediately errors "externalRef cycle detected."

**Sound in `rng`:** no data races / no mutable global parse state (verified `-race`, 50 concurrent parses); not XXE-exploitable; datatypeLibrary inheritance, combine merging, and §4.16 name-class constraints are comprehensive.

## Appendix B — `validator` package findings (V1–V28)

V1–V4 expanded in §3. Remaining (all CONFIRMED unless noted):

- **V5** (High) pattern.go:2540 — interleave child matched once against one contiguous span; `interleave(oneOrMore a, b)` rejects `<a/><b/><a/>`. Legacy engine matches by local name only, never validates matched children (validator.go:2099 admits it).
- **V6** (High) pattern.go:2466 — on interleave failure, consumed tokens are prepended back and `pos=0`, duplicating tokens and invalidating outstanding `Mark()`s; `choice(interleave(a,b), interleave(a,c))` rejects valid `<a/><c/>`.
- **V7** (High) validator.go:74 — `LineTracker` counts bytes at the decoder's buffered read position; a line-1 error in a 43-line doc reports L43. `decoder.InputPos()` unused. Column counts bytes not runes. *Independently reproduced in this audit* (error on line 4 reported as line 5).
- **V8** (High) pattern.go:2188 — `matchElementPat` compares local name only; `ElementPat.Ns`/`AnyName`/`NsName` never consulted; child in a different namespace accepted.
- **V9** (High) validator.go:2477 — `validateDataType` `default: return true`; `date`/`dateTime`/`hexBinary`/`NCName`/… accept anything; `byte`/`short`/`int` no range check; `decimal` via `ParseFloat` accepts `Inf`/`NaN`.
- **V10** (High) validator.go:2597 — `regex.MatchString` unanchored; `pattern=[0-9]{3}` accepts `abc123def`. Comment at 2605 falsely claims Go regexp "uses backtracking with memoization."
- **V11** (Medium) validator.go:246 — returns right after root; trailing junk (`</extra>`) never read; AST buffer errors surface as `ValidationError` with `err==nil`; legacy paths swallow decoder errors → truncated docs validate clean.
- **V12** (Medium) validator.go:234 — empty/whitespace/root-less input returns `(nil,nil)`.
- **V13** (Medium) validator.go:36 — `MaxInterleave`/`CollectUnknown` never read; `MaxDepth` dead in default AST mode (500-deep doc passes with `MaxDepth:100`); `FailFast`/`MaxErrors` cap recording, not work.
- **V14** (Medium) pattern.go:1774 — `NewTokenBuffer` copies every token of the root's content; no `io.LimitReader`, no `context.Context`; interleave recurses per token.
- **V15** (Medium) official_suite.go:475 — 1203-line harness shipped in the importable package; Invalid-doc test "passes" on *any* error, inflating the "100%" claim.
- **V16** (Medium) pattern.go:661 — permissive fallbacks: `getStartPattern` synthesizes accept-all wildcards; `AnyContentPat` accepts anything for unrecognized content; `MatchPattern` `default:` returns success. Comment at 661 falsely claims empty element = mixed content.
- **V17** (Medium; code CONFIRMED, impact PLAUSIBLE) pattern.go:1747 — `readInnerXML` re-serializes fragments without escaping `"`/`&`/`<` and drops namespaces/prefixes.
- **V18** (Low-Med) pattern.go:1952 — `matchChoicePat` discards a succeeding alternative if content remains and it isn't last → order-sensitive choice.
- **V19** (Low) validator.go:2628 — `regexCache` capped at 100, then recompiles each call; global, unevicted; part of the V4 race.
- **V20** (Low) validator.go:2380 — `countListDataPatterns` counts `<data` substrings via `strings.Index`; miscounts on comments/nesting/`<database>`.
- **V21** (Low) whole-feature duplication with divergent semantics (`validateInterleave` vs `matchInterleavePat`, token normalization differs by path).
- **V22** (Low) pattern.go:188 — dead: `AttributePat` never built; `AnyName`/`NsName` never assigned; stub `validateListChoice`.
- **V23** (Low-Med) validator.go:449 — `ValidationError.Expected`/`Found` usually empty; `Element` sometimes gets a dot-path; `Path` has no namespace/index (two `<item>` siblings indistinguishable).
- **V24** (Low) validator.go:263 — `oneOrMoreChoiceAttributeMatched` is one context-global flag; attribute match can satisfy a `oneOrMore` content requirement; nested visits clobber the parent's flag.
- **V26** (Low) validator.go:216 — no `CharsetReader`; `ISO-8859-1` docs fail. Positive: internal DTD entities error rather than resolve — no XXE.
- **V27** (Low, meta) tests `t.Logf` instead of asserting (nameclass_test, linenumber_test); "concurrent" test avoids the racy path.
- **V28** (Low) validator.go:1725 — legacy path: `validateOptional`/`validateChoiceElements` skip rest of parent on match/mismatch; `parseInt` accepts `"12abc"`.

**Sound in `validator`:** root-element attribute validation (incl. name-class except logic), the facets that exist (minLength/maxLength/min|maxInclusive|Exclusive/fractionDigits), `value` token normalization + `type="string"` exact match, no XXE.

## Appendix C — `generator` & tooling findings (G1–G20)

- **G1** (High, CONFIRMED) types.go:255 — `collectNestedElements` recurses Group/OneOrMore/ZeroOrMore/Optional but not `elem.Elements`; a 3-deep direct nesting emits a field of an undefined type (`undefined: B`).
- **G2** (High, CONFIRMED) types.go:37 — `buildTypeFromElement` has no pass for `elem.Choice`/`elem.Interleave`; the choice/interleave schemas generate structs with only `XMLName`; unmarshal silently drops all data.
- **G3** (High, CONFIRMED) types.go:471 — `ref` fields typed/tagged from the *define* name; when define≠element name, produces `undefined: P1` and tags that never match documents. Works in-repo only because every test schema uses define==element name.
- **G4** (Medium, CONFIRMED) types.go:193 — same-name/different-content elements: first wins, second silently discarded.
- **G5** (High, CONFIRMED) generate-benchmark-projects/main.go:246 — `filepath.Base(string(xmlContent))` uses document *content*; every generated `types_test.go` has a syntactically invalid benchmark name; the whole `make bench` generator pipeline measures nothing.
- **G6** (Med-High, CONFIRMED) unmarshaler.go:160 — exported `<Root>Validator`/`<Root>Once` globals + unconditional `formatValidationErrors`; two schemas in one package fail to compile (`formatValidationErrors redeclared`). *Independently reproduced this audit.*
- **G7** (Medium, CONFIRMED) unmarshaler.go:35 — `panic` inside `sync.Once`; because `Once.Do` marks done on panic, the first caller panics and all subsequent callers proceed with nil validator → validation silently skipped.
- **G8** (Medium, CONFIRMED) types.go:206 — map-iteration ordering → nondeterministic generated output between runs.
- **G9** (Low-Med, CONFIRMED) dummy/simple.go:1 — `package XXX`, untracked, current-format generator output with a debug leftover; silently compiled by `go build ./...`.
- **G10** (Medium, CONFIRMED) official_suite_test.go:295 — roundtrip test marshals `root` against itself; never re-parses; can't catch G2–G4.
- **G11** (Medium, CONFIRMED) generation_test.go:118 — tests `_ = err`/only touch `XMLName`; never assert the dropped fields exist.
- **G12** (Low, CONFIRMED) helpers_test.go:10 — duplicate `sanitizeIdentifier` without keyword handling.
- **G13** (README audit, CONFIRMED) — see X3; also: required child `<data type="int"/>` stays `string` (types.go:362 never consults `subElem.Data`); `ns=` ignored (tags carry no namespace).
- **G14** (Low-Med, CONFIRMED) types.go:786 — `schemaContent` param dead; code embeds `SerializeGrammar(grammar)` instead; doc contradicts impl; feeds G7's panic path via any serializer bug.
- **G15** (Medium, PLAUSIBLE) types.go:406 — `getAttributeFieldType` reads the element's `<list>` to type an attribute → `[]int64` attribute → runtime unmarshal error.
- **G16** (Medium, CONFIRMED) run_fuzzing.sh:17 — runs `go test -fuzz` from repo root (no package) → `no Go files`; `|| true` hides it; the three fuzz targets don't exist.
- **G17** (Medium, CONFIRMED) profile.sh:17 — `-cpuprofile` with `./...` → `cannot use -cpuprofile flag with multiple packages`; all three steps abort.
- **G18** (Low, CONFIRMED) extract-test/extract.go:51 — hardcoded `../../testdata/official/spectest.xml`; dir doesn't exist and `../../` from `cmd/internal/extract-test` resolves to `cmd/`.
- **G19** (Low, CONFIRMED) — stale path hints: official-tests/main.go:30 (`cmd/official-tests`), benchmarks doc (`./cmd/generate-benchmark-projects`); a third divergent copy of `toGoTypeName` in generate-benchmark-projects/main.go:310.
- **G20** (Low, CONFIRMED/PLAUSIBLE) — ignored `EncodeToken`/`Flush` errors; shipped debug line `_ = rawBuf.String()`; error messages embed the whole document; EOF mid-element leaves unbalanced XML; `<n> 42 </n>` chardata int fails (no trim).

**Sound in `generator`/tooling:** `cmd/generate` flags (`-schema`/`-package`, stdout) work as documented; cardinality→slice and optional→pointer+omitempty hold; attribute/optional-element type mapping works; official suite tool runs 742/742 (lenient — see V15).
