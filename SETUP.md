# Setup — first push and first release

The Go module path is already `github.com/AlexandruIspas659/ipcprobe`.

## 1. Build and try it (optional — there's a prebuilt `ipcprobe` in this folder already)

```bash
cd ~/Documents/dev/ipcprobe
go build -o ipcprobe ./cmd/ipcprobe      # standard library only — works offline
go test ./...
./ipcprobe list --iface en0
```

## 2. Create the two GitHub repos (empty, no README — the files are here)

- `AlexandruIspas659/ipcprobe` — public.
- `AlexandruIspas659/homebrew-tap` — public. Homebrew requires exactly this name (`homebrew-` prefix) for
  `brew install AlexandruIspas659/tap/ipcprobe` to resolve. Contents are in `homebrew-tap.zip`.

## 3. Push both

```bash
# main repo
cd ~/Documents/dev/ipcprobe
git init -b main
git add .
git commit -m "ipcprobe 0.1.0: MHED protocol spec, Go CLI, Python reference"
git remote add origin git@github.com:AlexandruIspas659/ipcprobe.git
git push -u origin main

# tap repo (unzip homebrew-tap.zip next to it first)
cd ~/Documents/dev/homebrew-tap
git init -b main
git add .
git commit -m "tap: ipcprobe (head formula; GoReleaser will version it)"
git remote add origin git@github.com:AlexandruIspas659/homebrew-tap.git
git push -u origin main
```

Real captures (*.pcap/*.pcapng) and the local `ipcprobe` binary are git-ignored — `git add .` is safe.

## 4. One secret, so releases can update the tap

GoReleaser runs in the ipcprobe repo's CI but has to commit into the *tap* repo, which the default token can't do.

1. GitHub → Settings (your profile) → Developer settings → Personal access tokens → **Fine-grained tokens** →
   Generate. Repository access: **only** `homebrew-tap`. Permissions: **Contents: Read and write**. Expiry: your call.
2. ipcprobe repo → Settings → Secrets and variables → Actions → **New repository secret**:
   name `HOMEBREW_TAP_TOKEN`, value = the token.

## 5. Cut the release

```bash
cd ~/Documents/dev/ipcprobe
git tag v0.1.0
git push --tags
```

`.github/workflows/release.yml` runs the tests, then GoReleaser builds macOS (universal) + Linux + Windows,
publishes the GitHub release with the notes from `.goreleaser.yaml` (`release.header`), attaches the archives and
`checksums.txt`, and pushes `Formula/ipcprobe.rb` to the tap. A few minutes later, on any Mac:

```bash
brew install AlexandruIspas659/tap/ipcprobe
ipcprobe --version
```

## Next versions

Bump `CHANGELOG.md`, update the `release.header` block in `.goreleaser.yaml` if the headline changed, then
`git tag v0.1.1 && git push --tags`. Tags with a hyphen (`v0.2.0-rc1`) are marked pre-release and don't update the tap.
