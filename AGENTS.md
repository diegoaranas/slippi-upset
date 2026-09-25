# AGENTS.md

## Commits must follow Conventional Commits

Releases are fully automated. Every push to `main` runs
[semantic-release](https://semantic-release.gitbook.io), which reads the commit
messages since the last tag to decide the next version and creates the GitHub
release with Windows, Linux and macOS binaries attached (`.github/workflows/release.yml`,
the `release` config in `package.json`, `tools/build`). `package.json` exists only for
this release tooling; the app itself is plain Go and needs no Node.

**A commit that doesn't follow the format is invisible to the release builder:**
it ships no release and is left out of the release notes. So every commit on
`main` must look like:

```
<type>(<optional scope>): <short summary, lowercase, no period>
```

| Type | Release | Use for |
|---|---|---|
| `feat` | minor (1.**2**.0) | new user-facing behavior |
| `fix`, `perf`, `revert` | patch (1.2.**1**) | bug fixes, speedups, reverting a commit |
| a `BREAKING CHANGE:` footer, with any type | major (**2**.0.0) | changes that break config, CLI flags or saved files |
| `docs`, `refactor`, `test`, `build`, `ci`, `chore`, `style` | none | everything else |

Examples:

```
feat: play a sound when a friendly opponent joins
fix(replay): read placements from older replays
feat: rename --test to --check

BREAKING CHANGE: --test was removed.
```

**Don't use the `!` shorthand** (`feat!: ...`). semantic-release's default parser
can't read it, so the commit would ship no release at all. Always mark breaking
changes with the `BREAKING CHANGE:` footer.

## Pull requests are squash-merged

Squashing turns the whole PR into a single commit on `main`: the **PR title**
becomes its header, and the body is whatever is in GitHub's merge dialog. So:

- The PR title must follow the format above, and its type sets the release: a PR
  titled `ci: ...` ships nothing, even if it contains a `feat` commit.
- For a breaking change, write `BREAKING CHANGE: <what broke>` in the commit body
  in the merge dialog before confirming.
- Commits inside the PR branch can be messy; they are discarded on merge.

## Before committing

- `gofmt -l .` prints nothing.
- `go vet ./...` passes, and also with `GOOS=windows` and `GOOS=darwin`: each
  platform has its own `platform_<os>.go` file.
- `go test ./...` passes.
- Use only the standard library plus `golang.org/x/sys` and `github.com/gopxl/beep/v2`
  (audio). Build with cgo off (`CGO_ENABLED=0`) so cross-compiling needs no C toolchain;
  keep `github.com/ebitengine/oto/v3` at v3.5.0 or later, the first release that needs no
  cgo on Linux.
