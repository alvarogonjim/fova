# Releasing fova

This page is the runbook for cutting a fova release. It assumes you have
push access to `alvarogonjim/fova` and `alvarogonjim/homebrew-tap`.

## One-time GitHub setup

1. **Create the tap repo.** Make `alvarogonjim/homebrew-tap` exist and contain
   at least an empty `Formula/` directory. Goreleaser will commit the first
   `Formula/fova.rb` on the first successful release.
2. **Create the `HOMEBREW_TAP_TOKEN` repo secret.** In
   `alvarogonjim/fova` → *Settings* → *Secrets and variables* → *Actions*,
   add a secret named `HOMEBREW_TAP_TOKEN`. Its value is a fine-grained
   personal access token with **Contents: write** permission on the
   `homebrew-tap` repo (and nothing else). The default `GITHUB_TOKEN` cannot
   push to a different repository, so this PAT is required.
3. **Verify the workflow has write access.** *Settings* → *Actions* →
   *General* → *Workflow permissions* → "Read and write permissions". The
   release workflow's `permissions.contents: write` block grants this at the
   job level too, but the org/repo default has to allow it.

## Cutting a release

```bash
# 1. Make sure master is green.
git checkout master
git pull --ff-only
go build ./... && go test ./... && go vet ./...
gofmt -l internal cmd pkg   # must print nothing

# 2. Bump the dev-version stub if needed (the link-time -X overrides it at
#    release time, but having the in-source value match the tag prevents
#    confusion if anyone builds from source without -ldflags).
$EDITOR internal/version/version.go
git commit -am "chore: bump version to 0.5.0"

# 3. Tag and push. The tag pattern v[0-9]+.[0-9]+.[0-9]+* covers final
#    releases and prereleases (e.g. v0.5.0-rc1).
git tag -a v0.5.0 -m "v0.5.0"
git push origin master
git push origin v0.5.0
```

That push fires `.github/workflows/release.yml`, which:

1. Checks out the repository at the tag with full history.
2. Installs Go (matching the toolchain version in `go.mod`).
3. Runs `goreleaser release --clean`, which
   * cross-builds the five binaries (darwin/linux × amd64/arm64, plus windows-amd64),
   * archives each into `tar.gz` (Unix) or `zip` (Windows),
   * writes `checksums.txt`,
   * creates a GitHub Release at the tag,
   * uploads the archives, checksums, and a changelog,
   * commits the bumped `Formula/fova.rb` to `alvarogonjim/homebrew-tap`.

Watch the run in the *Actions* tab. A successful run takes about 4–6 minutes.

## After the release

* Confirm the GitHub Release exists at
  `https://github.com/alvarogonjim/fova/releases/tag/<tag>` and lists five
  archives plus `checksums.txt`.
* Confirm the formula PR (or direct commit) landed in
  `https://github.com/alvarogonjim/homebrew-tap/blob/master/Formula/fova.rb`.
* Smoke-test the one-liner installer on a fresh machine:
  ```sh
  curl -fsSL https://fova.dev/install | sh
  fova version
  ```
* Smoke-test the Homebrew tap:
  ```sh
  brew tap alvarogonjim/tap
  brew install fova
  fova version
  ```

## Cutting a prerelease

Same flow with an `-rc<N>` suffix on the tag. Goreleaser marks the GitHub
Release `prerelease` automatically (the `release.prerelease: auto` setting),
and the formula bump still goes to the tap — users can pin to a specific
version with `brew install fova@0.5.0-rc1` if needed.

## Recovering from a failed release

* If goreleaser fails partway through, delete the bad tag (`git tag -d <tag>;
  git push --delete origin <tag>`), delete the (probably empty) GitHub
  Release, and re-tag once the fix lands.
* If only the tap commit failed (token expired, etc.), re-running the workflow
  is safe — goreleaser is idempotent on archives that already exist and will
  retry the formula push.

## Secrets checklist

| Secret | Scope | Used by |
|---|---|---|
| `GITHUB_TOKEN` (built-in) | this repo, `contents: write` | uploading archives to the GitHub Release |
| `HOMEBREW_TAP_TOKEN` (manual) | `homebrew-tap` repo, `contents: write` | committing `Formula/fova.rb` to the tap |

Both secrets live in **GitHub repository secrets** — they must not be checked
into the repo, embedded in a workflow, or distributed to contributors.

## Local dry-run

To check `.goreleaser.yaml` without publishing:

```sh
goreleaser check                           # schema-validate the config
goreleaser build --snapshot --clean        # cross-build the five binaries
goreleaser release --snapshot --clean --skip=publish
```

The third form produces a complete `dist/` directory (archives, checksums,
Linux/Windows binaries) without touching GitHub.
