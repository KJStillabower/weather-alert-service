# Issue: Simplify Cursor Rules to Reduce Overhead and Context Confusion

**Labels:** maintenance, technical-debt

## Summary

Current rule set has 14 files (~2174 lines) all marked `alwaysApply: true`, creating high token overhead, cross-reference complexity, and maintenance burden. Propose consolidation and simplification.

## Current State

- **14 rule files**, all `alwaysApply: true` (~2174 lines total)
- **Cross-references:** Rules reference each other (e.g., "per 040-testing.mdc", "see 090-security.mdc")
- **Overlap:** `030-service-pattern.mdc` and `080-go-patterns.mdc` both cover Go patterns, handlers, dependency injection
- **Meta-rules:** `020-rule-standards.mdc` adds version tracking overhead to every file
- **Token cost:** ~8700 tokens just for rules (2174 lines × ~4 tokens/line)

## Problems

1. **High overhead:** All 14 files loaded into context every request
2. **Context confusion:** Cross-references create circular dependencies; unclear which rule takes precedence
3. **Redundancy:** Same patterns explained in multiple places (e.g., handlers in both 030 and 080)
4. **Maintenance burden:** Version tracking, `lastUpdated` dates need constant updates
5. **Diminishing returns:** More rules ≠ better guidance; can create conflicting signals

## Proposed Simplification

### Option 1: Consolidate Overlapping Rules

**Merge:**
- `030-service-pattern.mdc` + `080-go-patterns.mdc` → single `go-service-patterns.mdc` (remove duplicate handler/DI/error handling)
- `100-communication.mdc` + `021-change-control.mdc` → single `git-commits.mdc` (commit messages are communication)

**Result:** 14 → 12 files

### Option 2: Reduce Always-Apply Scope

**Keep `alwaysApply: true` for:**
- `000-goal.mdc` - Core scope
- `010-role.mdc` - AI behavior
- `022-preserve-existing.mdc` - Critical safety rule

**Make context-specific:**
- `040-testing.mdc` - Only when editing `*_test.go` files (`globs: **/*_test.go`)
- `050-observability.mdc` - Only when editing observability code (`globs: **/observability/**`)
- `070-api-contract.mdc` - Only when editing handlers (`globs: **/http/**`)
- `090-security.mdc` - Only when editing code that touches secrets/config

**Result:** 3 always-apply + 11 context-specific = lower baseline overhead

### Option 3: Remove Meta-Rules

**Remove or simplify:**
- `020-rule-standards.mdc` - Version tracking adds overhead; rules are living docs, not APIs
- Keep frontmatter minimal: `description` only; drop `version`, `lastUpdated` unless needed

**Result:** Less maintenance, simpler structure

### Option 4: Extract Examples to Separate Files

**Move verbose examples:**
- `010-role.mdc` has 207 lines with many examples
- Extract examples to `docs/examples/` or inline only when essential
- Keep rules focused on principles, not exhaustive examples

**Result:** Smaller rule files, examples available when needed

## Risks

- **Breaking changes:** If rules are referenced in code/comments, consolidation could break references
- **Lost nuance:** Merging might lose important distinctions
- **Testing:** Need to verify AI behavior doesn't regress after simplification

## Recommended Approach

**Combine Options 1 + 2 + 3:**

1. **Merge overlapping rules** (14 → 12 files)
2. **Reduce always-apply** (3 core + 9 context-specific)
3. **Simplify meta-rules** (drop version tracking overhead)
4. **Extract verbose examples** (keep rules concise)

**Target:** ~800-1000 lines total, 3 always-apply rules, clearer boundaries

## Acceptance Criteria

- [ ] Overlapping rules consolidated (030+080, 100+021)
- [ ] Only 3 core rules marked `alwaysApply: true`
- [ ] Context-specific rules use `globs` for targeted loading
- [ ] Version tracking removed or simplified
- [ ] Total rule lines reduced by ~40-50%
- [ ] Cross-references minimized or removed
- [ ] Documentation updated if rule structure changes

## References

- Current rules: `.cursor/rules/*.mdc`
- Rule standards: `.cursor/rules/020-rule-standards.mdc`

## Risk Mitigation

### Code Scan of Rule Syntax

#### Summary

**No code references:** No `.go` files reference rule files.

**Documentation references:** Found in markdown files only.

#### External References by File

##### docs/About.md
**Lines 71-83:** Lists all 14 rules in a table:
- `000-goal.mdc`
- `010-role.mdc`
- `020-rule-standards.mdc`
- `021-change-control.mdc`
- `030-service-pattern.mdc`
- `040-testing.mdc`
- `050-observability.mdc`
- `060-reliability.mdc`
- `070-api-contract.mdc`
- `080-go-patterns.mdc`
- `090-security.mdc`
- `100-communication.mdc`
- `101-documentation.mdc`

**Impact:** If rules are consolidated/renamed, this table needs updating.

##### docs/observability.md
**Line 114:** `see 090-security.mdc` (inline reference)
**Line 306:** `050-observability.mdc` (in References table)

**Impact:** Low - inline references can be updated or removed if content is merged.

##### docs/issue-health-state-transition-logging.md
**Line 11:** `Per 050-observability.mdc` (rationale reference)

**Impact:** Low - reference provides context but not critical.

##### docs/issue-improved-observability-documentation.md
**Lines 29, 38, 111:** `050-observability.mdc` (source citations)

**Impact:** Low - historical reference in issue doc.

##### docs/issue-simplify-cursor-rules.md
**Multiple lines:** References many rules (this is the simplification issue itself)

**Impact:** N/A - this doc will be updated as part of simplification.

#### Cross-References Within Rules

Rules reference each other extensively:
- `010-role.mdc` → `000-goal.mdc`, `090-security.mdc`
- `030-service-pattern.mdc` → `000-goal.mdc`, `040-testing.mdc`
- `040-testing.mdc` → `060-reliability.mdc`, `070-api-contract.mdc`
- `050-observability.mdc` → `000-goal.mdc`, `090-security.mdc`
- `060-reliability.mdc` → `040-testing.mdc`, `050-observability.mdc`
- `070-api-contract.mdc` → `040-testing.mdc`
- `080-go-patterns.mdc` → `070-api-contract.mdc`, `050-observability.mdc`, `090-security.mdc`, `040-testing.mdc`
- `021-change-control.mdc` → `100-communication.mdc`, `090-security.mdc`, `050-observability.mdc`, `060-reliability.mdc`
- `101-documentation.mdc` → `070-api-contract.mdc`

**Impact:** High - consolidation will require updating these cross-references or removing them.

#### Findings

1. **No code dependencies:** No `.go` files reference rules. Safe to consolidate without breaking code.
2. **Documentation only:** References are in markdown docs, easily updated.
3. **Most referenced rules:**
   - `050-observability.mdc` - Referenced in 4 external docs + multiple rules
   - `040-testing.mdc` - Referenced in 3 rules
   - `090-security.mdc` - Referenced in 3 rules + 1 external doc
   - `000-goal.mdc` - Referenced in 3 rules (core scope)
   - `070-api-contract.mdc` - Referenced in 2 rules + 1 external doc
4. **Least referenced:** `022-preserve-existing.mdc`, `060-reliability.mdc` (only in rules, not external docs)

#### Consolidation Safety

**Safe to consolidate:**
- Rules with no external references: `022-preserve-existing.mdc`, `060-reliability.mdc`
- Rules only referenced in other rules: Most can be merged if cross-refs are updated

**Requires doc updates:**
- `docs/About.md` - Update rule list table
- `docs/observability.md` - Update inline references
- Cross-references within rules themselves

**Recommendation:** Consolidation is safe; only documentation updates needed. No code changes required.