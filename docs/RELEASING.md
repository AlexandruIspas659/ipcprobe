# How a release happens

What actually runs, in order, when you type `git tag v0.1.1 && git push --tags` — and why each piece is there.
Two files drive it: `.github/workflows/release.yml` (the GitHub Actions job) and `.goreleaser.yaml` (what
GoReleaser does inside that job).

## 1. The trigger — `release.yml`

```yaml
on:
  push:
    tags: ["v*"]
```

GitHub Actions watches the repository for events. This workflow ignores ordinary commits (the separate `ci.yml`
handles those) and fires only when a **tag** matching `v*` is pushed. A tag is just a name pointing at a commit;
pushing one is the release button. Nothing about the commit itself is special — the same commit can sit on `main`
for days, and the release starts the moment a `v0.1.1` label is attached and pushed.

```yaml
permissions:
  contents: write
```

Every workflow run gets an automatic short-lived credential, `GITHUB_TOKEN`, scoped to *this* repository. By default
it can only read. Creating a release and uploading files is a write to the repository's contents, so the job asks
for that. This token is what GoReleaser uses to create the GitHub release. It cannot touch any other repository —
which is the whole reason the second token exists (below).

## 2. The job, step by step

```yaml
runs-on: ubuntu-latest
```

GitHub starts a fresh Linux virtual machine. Everything below happens inside it and is thrown away afterwards; nothing
is built on your Mac.

**`actions/checkout@v4` with `fetch-depth: 0`.** Clones the repository at the tagged commit. The default checkout is
shallow (just the one commit) to save time; `fetch-depth: 0` fetches the full history and all tags. GoReleaser needs
that to find the *previous* tag and generate the changelog from the commits in between — the v0.1.0 log said
"couldn't find any tags before v0.1.0, using git", which is this mechanism working on a first release.

**`actions/setup-go@v5` with `go-version: "1.23"`.** Installs the Go toolchain. The version pinned here is what
compiles the release binaries, independent of what's on your laptop.

**`go test ./...`.** Runs the test suite — including the vectors conformance test — before anything is built. If a
test fails, the job stops here, no release is created, and the tag is left pointing at a commit that never shipped.
Delete the tag, fix, re-tag.

**`goreleaser/goreleaser-action@v6` with `args: release --clean`.** Downloads GoReleaser (the `~> v2` constraint means
"the latest 2.x") and runs it. `--clean` empties the `dist/` directory first so nothing stale from a previous run
leaks in. Two environment variables are passed:

```yaml
env:
  GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}          # automatic; for THIS repo
  HOMEBREW_TAP_TOKEN: ${{ secrets.HOMEBREW_TAP_TOKEN }}  # the one you created; for the tap repo
```

`secrets.X` is how a workflow reads a value stored in the repository's Settings → Secrets. The value never appears in
logs. The v0.1.0 failure was exactly this line being absent: the secret existed in GitHub but the workflow never
handed it to the process, so GoReleaser's template `{{ .Env.HOMEBREW_TAP_TOKEN }}` had nothing to read.

## 3. Inside GoReleaser — `.goreleaser.yaml`

GoReleaser is a single tool that turns "a Go module at a tag" into "binaries for every platform, a GitHub release
with notes, checksums, and a package-manager entry", from one declarative file. It runs as a fixed pipeline; the
sections of the YAML configure the stages. In order:

### `before.hooks: go mod tidy`

Runs before building. With no dependencies this is a no-op, but it guarantees `go.mod`/`go.sum` are consistent with
the source, so a stray import can't produce a release that differs from what `go build` gives locally.

### `builds`

```yaml
main: ./cmd/ipcprobe
binary: ipcprobe
env: [CGO_ENABLED=0]
goos:   [darwin, linux, windows]
goarch: [amd64, arm64]
ldflags: -s -w -X main.version={{.Version}} -X main.commit={{.ShortCommit}}
```

GoReleaser cross-compiles the matrix — 3 operating systems × 2 architectures = 6 binaries — on the one Linux VM.
Go can do that natively; no macOS or Windows machine is involved. What the flags mean:

- `CGO_ENABLED=0` forbids any C code, which forces a **fully static** binary: no libc dependency, so the Linux build
  runs on any distribution (or a UniFi gateway) and the macOS build has no dynamic library surprises.
- `-s -w` strip the symbol table and DWARF debug info. That's the difference between ~3.5 MB and ~2.4 MB; stack
  traces still work, only debugger symbols are gone.
- `-X main.version={{.Version}}` sets the `version` variable in `main.go` at link time. `{{.Version}}` is a GoReleaser
  template evaluated from the tag (`v0.1.1` → `0.1.1`). This is how the binary knows its own version without the
  source containing it.

### `universal_binaries` with `replace: true`

macOS has two CPU architectures in the wild. Rather than making users pick, GoReleaser merges the `darwin/amd64` and
`darwin/arm64` binaries into one **universal** (fat) binary — the same thing Apple's `lipo` tool does — and
`replace: true` means the two single-architecture macOS builds are dropped from the output. That's why the release
has `ipcprobe_darwin_all.tar.gz` and no `darwin_amd64`/`darwin_arm64`.

### `archives`

Each binary is packed with `LICENSE`, `README.md` and `PROTOCOL.md` into `ipcprobe_<os>_<arch>.tar.gz` (`.zip` for
Windows, because that's what Windows users can open). The `name_template` fixes the filenames, which matters because
the Homebrew entry is generated with these exact names.

### `checksum`

Writes `checksums.txt`: a SHA-256 for every archive. Two uses — a user can verify a download, and GoReleaser reads it
back to put the correct hash into the Homebrew formula/cask, so `brew` will refuse a tampered or corrupted download.

### `release`

Creates the GitHub release for the tag using `GITHUB_TOKEN` and uploads the archives and checksums as assets.

- `draft: false` — published immediately. It has to be: the Homebrew entry generated in the next stage points at
  these asset URLs, and draft-release assets are not publicly downloadable.
- `prerelease: auto` — a tag like `v0.2.0-rc1` (anything with a hyphen) is marked pre-release; `v0.1.1` is not.
- `name_template` — the title shown on the release page.
- `header` / `footer` — the human-written body. The header is the text you see on the release page: what it is, the
  install command, usage. Between header and footer GoReleaser inserts the **changelog**.

### `changelog`

`use: github` builds the list of changes from the commits between the previous tag and this one, via the GitHub API
(on the very first release there is no previous tag, so it falls back to `git log`). The `filters.exclude` patterns
drop noise commits — anything starting with `docs:`, `chore:`, `ci:` — so the list is only user-relevant changes.
This is why commit messages matter: they *are* the release notes' middle section.

### The Homebrew stage — currently a formula, moving to a cask

Homebrew installs from a **tap**: a Git repository named `homebrew-<something>` containing Ruby files that describe
where to download a package and how to install it. Ours is `AlexandruIspas659/homebrew-tap`, so users write
`AlexandruIspas659/tap/ipcprobe` (Homebrew inserts the `homebrew-` prefix).

GoReleaser generates that Ruby file for us on every release: it renders a template with the release's download URL
for the macOS archive, the SHA-256 from `checksums.txt`, the version, homepage, description and license, then
**commits it into the tap repository**. That commit is why a second credential exists: `GITHUB_TOKEN` can only write
to `ipcprobe`, and the tap is a different repository. `HOMEBREW_TAP_TOKEN` is a fine-grained personal access token
with *Contents: read & write* on `homebrew-tap` only — the narrowest thing that can make that commit.

What `brew install` then does: reads the Ruby file from the tap, downloads the archive from the GitHub release,
verifies the SHA-256, unpacks the binary into the Cellar, and symlinks it onto your `PATH`. `brew upgrade` compares
the version in the tap file with what's installed. Every release therefore updates every user with no work on
your part beyond pushing a tag.

**Formula vs cask.** A *formula* traditionally describes building software from source; a *cask* describes
installing a prebuilt application. v0.1.1 was published with GoReleaser's `brews` section, which generates a formula
that merely copies a prebuilt binary — GoReleaser now calls that approach deprecated and has replaced it with
`homebrew_casks`, which is what the current `.goreleaser.yaml` in this repository contains. The cask has one practical
advantage for us: it can strip macOS's quarantine attribute at install time (the `hooks.post.install` block), which
an unsigned binary otherwise trips over. The switch is pending because a tap cannot hold a formula and a cask with
the same name, so the old file must be deleted in the same step — the exact sequence is in
[SETUP.md](../SETUP.md).

## 4. The other workflow — `ci.yml`

Runs on every push to `main` and on pull requests: checks out, sets up Go, runs `go vet`, `go build ./...` and `go test ./...` (including the end-to-end fake-camera test).
It publishes nothing. Its job is to make the little green tick on a commit mean "this compiles and passes the
vectors", so a broken commit is caught long before anyone tags it.

## 5. Making a release, in practice

1. Update `CHANGELOG.md` with a section for the new version. If the headline text changed, update `release.header`
   in `.goreleaser.yaml` too.
2. Commit and push to `main`; wait for `ci` to go green.
3. `git tag vX.Y.Z && git push --tags`.
4. Watch Actions → `release`. Two to three minutes later the release page exists with six assets and the tap has a
   new commit.
5. `brew upgrade ipcprobe` (or a fresh install) on any Mac picks it up.

If the release job fails *after* "release published" but before the Homebrew stage — as v0.1.0 did — the GitHub
release is real and usable; only the Homebrew entry is missing. Fix the cause and tag the next patch version rather
than re-using the tag: a re-run executes the workflow file as it was at that tag's commit, so it cannot pick up a
fix to the workflow itself.

## 6. Secrets and what can go wrong

| Symptom | Cause |
|---|---|
| `map has no entry for key "HOMEBREW_TAP_TOKEN"` | The secret isn't passed in `release.yml`'s `env:` block, or isn't defined in repo Settings → Secrets. |
| Homebrew stage says `403`/`404` on push | The token lacks *Contents: write* on the tap, or was scoped to the wrong repository, or has expired. |
| `brew install` says the file doesn't exist | Local tap clone is stale: `brew untap alexandruispas659/tap` then install again. Or the release used a different file type than the command (formula vs `--cask`). |
| Release created but assets missing | A build target failed; the log names it. Usually a Go version mismatch. |
| Job never starts | The tag doesn't match `v*` (e.g. `0.1.1` without the `v`), or Actions is disabled for the repo. |

The two tokens, side by side:

| | `GITHUB_TOKEN` | `HOMEBREW_TAP_TOKEN` |
|---|---|---|
| Who makes it | GitHub, automatically, per run | You, once, in Developer settings |
| Lifetime | The duration of the job | Until its expiry date |
| Scope | This repository only | The `homebrew-tap` repository only |
| Used for | Creating the release, uploading assets | Committing the formula/cask into the tap |
