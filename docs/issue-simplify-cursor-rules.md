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

## Risks

- **Breaking changes:** If rules are referenced in code/comments, consolidation could break references
- **Lost nuance:** Merging might lose important distinctions
- **Testing:** Need to verify AI behavior doesn't regress after simplification

## References

- Current rules: `.cursor/rules/*.mdc`
- Rule standards: `.cursor/rules/020-rule-standards.mdc`
