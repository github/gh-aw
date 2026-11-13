# ADR-77176: Preserve Relative Import Layout on Remote Add

**Date**: 2026-11-12
**Status**: Draft
**Deciders**: pelikhan (requested the change); [TODO: verify] other reviewers

---

### Context

`gh aw add` installs a remote workflow and fetches its frontmatter imports into the consuming repository. Imports are written relative to the source workflow (for example, `../prompts/review.md` from `.github/workflows/review.md`), but the previous implementation kept the full repository-relative path for any import outside the source workflow directory. The file landed at `.github/workflows/.github/prompts/review.md`, and strict compilation failed with `import file not found` (issue #77151). Because remote content chooses these write paths, the fix must keep every write inside the consuming repository and must not reopen the traversal and symlink-escape risks addressed for the import cache in [ADR-73582](73582-confine-import-cache-io-with-os-root.md).

### Decision

We will write each fetched import at the path obtained by resolving it relative to the source workflow directory (`filepath.Rel(originalBaseDir, remoteFilePath)`), so an import of `../prompts/review.md` installs at `.github/prompts/review.md`. Every write and read goes through an `os.Root` handle opened on the repository root, and destinations must stay inside the repository's `.github` directory (or the target directory when no git repository is found); unsafe destinations are rejected before any download. The primary driver is compiler compatibility, with confinement as a required constraint.

### Alternatives Considered

#### Alternative 1: Rewrite imports into the workflows directory

Fetch out-of-tree imports into a local dependency folder under `.github/workflows/` and rewrite the consuming workflow's `imports:` entries (and transitive relative imports inside fetched files) to match. This keeps all writes inside the workflows directory. It was not chosen because it edits user-authored frontmatter and changes the source layout instead of preserving it, which the issue's expected behavior requires. [TODO: verify; rejection rationale inferred from issue #77151, not discussed in the PR.]

#### Alternative 2: Fail closed on imports outside the workflows directory

Keep the previous boundary and reject any import that resolves outside `.github/workflows/`. This is the simplest and most conservative option, and it matches the old `targetDir` check. It was rejected because it does not fix the reported bug: the issue expects `../prompts/review.md` to install and compile successfully. This was a close call only on security grounds, and the root-confined approach addresses those grounds directly.

#### Alternative 3: Mirror the source repository's full `.github` tree

Download the source repository's entire `.github` directory so every relative path resolves. This avoids per-import path mapping, but it fetches unrelated files, widens the write surface, and increases API calls. [TODO: verify whether this option was considered during the PR; included for completeness.]

### Consequences

#### Positive
- Imports outside `.github/workflows/` install at the paths the compiler resolves, which fixes the layout reported in #77151.
- Root-confined I/O with `os.Root` rejects `..` traversal, absolute destinations, and symlink escapes (including dangling symlinks) before any download.
- Regression tests cover shared, external, nested/transitive, and custom-directory layouts, and confirm that the installed workflow compiles in strict mode.

#### Negative
- The write boundary widens from the target workflows directory to all of `.github/`, so a remote workflow can create files in sibling directories such as `.github/prompts/`. Existing files are skipped unless `--force` is set, which limits overwrites but not new file creation.
- Custom `--dir` installs can import only from within `.github/`; imports that resolve outside it fail with an "outside .github" error.
- Outside a git repository, confinement falls back to the target directory, which is stricter and may reject imports that would otherwise resolve.

#### Neutral
- Confinement depends on `gitutil.FindGitRootFrom` to locate the repository root, and on `os.OpenRoot` (Go 1.24+). [TODO: verify minimum Go version in `go.mod`.]
- The `.github` import root is a fixed convention. Repositories that keep shared content outside `.github/` are not served by this path mapping.
- Unit tests cover the layouts, but the end-to-end reproduction in #77151 (`gh aw add` against a live source repository followed by `gh aw compile --strict`) is not part of the PR's tests. [TODO: verify.]

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
