# Issue: Simplify Cursor Rules to Reduce Overhead and Context Confusion

**Labels:** maintenance, technical-debt

## Progress

✅ **Completed:**
- Consolidated `030-service-pattern.mdc` + `080-go-patterns.mdc` → `030-patterns.mdc`
- Consolidated `100-communication.mdc` + `021-change-control.mdc` + `101-documentation.mdc` → `100-documentation-communication.mdc`
- Removed `020-rule-standards.mdc` (unnecessary meta-rule)
- Updated `040-testing.mdc` to use `globs: **/*_test.go` instead of `alwaysApply: true`
- Updated `050-observability.mdc` to use `globs: **/observability/**` instead of `alwaysApply: true`
- Updated `070-api-contract.mdc` to use `globs: **/http/**` instead of `alwaysApply: true`
- Enhanced `040-testing.mdc` with inline test documentation example
- Updated `docs/About.md` to reflect new rule structure

**Remaining:**
- Evaluate remaining `alwaysApply: true` rules: `030-patterns.mdc`, `060-reliability.mdc`, `100-documentation-communication.mdc` (could potentially be context-specific)
- Extract verbose examples from rules (if needed)

## Summary

Current rule set has 14 files (~2174 lines) all marked `alwaysApply: true`, creating high token overhead, cross-reference complexity, and maintenance burden. Propose consolidation and simplification.

## Current State

- **11 rule files** (reduced from 14), 7 still use `alwaysApply: true` (4 core + 3 others), 3 context-specific (~2174 lines total)
- **Cross-references:** Rules reference each other (e.g., "per 040-testing.mdc", "see 090-security.mdc")
- **Consolidated:** `030-patterns.mdc` (merged 030+080), `100-documentation-communication.mdc` (merged 100+021+101)
- **Context-specific:** 
  - `040-testing.mdc` uses `globs: **/*_test.go` (loads only when editing test files)
  - `050-observability.mdc` uses `globs: **/observability/**` (loads only when editing observability code)
  - `070-api-contract.mdc` uses `globs: **/http/**` (loads only when editing HTTP handlers)
- **Enhanced:** `040-testing.mdc` includes inline test documentation example
- **Removed:** `020-rule-standards.mdc` (unnecessary meta-rule)
- **Token cost:** Reduced baseline overhead with 3 context-specific rules (load only when relevant files are edited)

## Problems

1. **High overhead:** 7 files still loaded into context every request (4 core + 3 others; reduced from 14)
2. **Context confusion:** Cross-references create circular dependencies; unclear which rule takes precedence
3. **Redundancy:** Same patterns explained in multiple places (e.g., handlers in both 030 and 080)
4. **Maintenance burden:** Version tracking, `lastUpdated` dates need constant updates
5. **Diminishing returns:** More rules ≠ better guidance; can create conflicting signals

## Proposed Simplification

### Option 1: Consolidate Overlapping Rules

**Merge:**
- ✅ `030-service-pattern.mdc` + `080-go-patterns.mdc` → single `030-patterns.mdc` (remove duplicate handler/DI/error handling) **COMPLETED**
- ✅ `100-communication.mdc` + `021-change-control.mdc` + `101-documentation.mdc` → single `100-documentation-communication.mdc` (commit messages are communication and documentation) **COMPLETED**

**Result:** 14 → 12 files (completed)

### Option 2: Reduce Always-Apply Scope

**Keep `alwaysApply: true` for:**
- `000-goal.mdc` - Core scope
- `010-role.mdc` - AI behavior
- `001-preserve-existing.mdc` - Critical safety rule
- `090-security.mdc` - Security is cross-cutting (secrets/config/logging/git apply everywhere)

**Make context-specific:**
- ✅ `040-testing.mdc` - Only when editing `*_test.go` files (`globs: **/*_test.go`) **COMPLETED**
- ✅ `050-observability.mdc` - Only when editing observability code (`globs: **/observability/**`) **COMPLETED**
- ✅ `070-api-contract.mdc` - Only when editing handlers (`globs: **/http/**`) **COMPLETED**
- ✅ `090-security.mdc` - Keep as `alwaysApply: true` (security is cross-cutting, applies everywhere) **DECISION: KEEP ALWAYS-APPLY**

**Result:** 3 always-apply + context-specific = lower baseline overhead

### Option 3: Remove Meta-Rules

**Remove or simplify:**
- ✅ `020-rule-standards.mdc` - Version tracking adds overhead; rules are living docs, not APIs **REMOVED**
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

1. ✅ **Merge overlapping rules** (14 → 11 files) **COMPLETED**
2. 🔄 **Reduce always-apply** (3 core + context-specific) **IN PROGRESS** - 3 rules now context-specific (`040`, `050`, `070`)
3. ✅ **Simplify meta-rules** (drop version tracking overhead) **COMPLETED** - `020-rule-standards.mdc` removed
4. [ ] **Extract verbose examples** (keep rules concise)

**Target:** ~800-1000 lines total, 4 core always-apply rules (goal, role, preserve-existing, security), clearer boundaries

## Acceptance Criteria

- [x] Overlapping rules consolidated (030+080 → `030-patterns.mdc`, 100+021+101 → `100-documentation-communication.mdc`)
- [ ] Only 4 core rules marked `alwaysApply: true` (currently: `000-goal`, `010-role`, `001-preserve-existing`, `090-security`; 3 others still use `alwaysApply`: `030-patterns`, `060-reliability`, `100-documentation-communication`)
- [x] Context-specific rules use `globs` for targeted loading (`040-testing.mdc`, `050-observability.mdc`, `070-api-contract.mdc` updated)
- [x] Version tracking removed or simplified (`020-rule-standards.mdc` removed)
- [ ] Total rule lines reduced by ~40-50%
- [ ] Cross-references minimized or removed
- [x] Documentation updated if rule structure changes (`docs/About.md` updated)

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
**Lines 71-82:** Lists all 12 rules in a table (updated):
- `000-goal.mdc`
- `010-role.mdc`
- `020-rule-standards.mdc`
- `030-patterns.mdc` (consolidated from 030+080)
- `040-testing.mdc`
- `050-observability.mdc`
- `060-reliability.mdc`
- `070-api-contract.mdc`
- `090-security.mdc`
- `100-documentation-communication.mdc` (consolidated from 100+021+101)

**Impact:** ✅ Updated to reflect consolidated rules.

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
- `030-patterns.mdc` → `000-goal.mdc`, `040-testing.mdc`, `050-observability.mdc`, `070-api-contract.mdc`, `090-security.mdc` (consolidated from 030+080)
- `040-testing.mdc` → `060-reliability.mdc`, `070-api-contract.mdc`
- `050-observability.mdc` → `000-goal.mdc`, `090-security.mdc`
- `060-reliability.mdc` → `040-testing.mdc`, `050-observability.mdc`
- `070-api-contract.mdc` → `040-testing.mdc`
- `100-documentation-communication.mdc` → `090-security.mdc`, `050-observability.mdc`, `060-reliability.mdc`, `070-api-contract.mdc` (consolidated from 100+021+101)

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