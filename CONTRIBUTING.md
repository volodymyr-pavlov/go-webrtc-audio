# Contributing

Bug fixes are welcome at any time. Anything bigger starts with an issue, so that the approach is agreed before the
work is done.

## How to send a change

1. The change is made on a branch of a fork of the project on GitHub.
2. It passes the checks of the workflow: `make verify`. They need Go 1.27, Node.js 24 and `ffmpeg`.
3. The commit message is in the [Conventional Commits](https://www.conventionalcommits.org/) style. `fix:` makes a
   patch release, `feat:` a minor release, and `feat!:` or a `BREAKING CHANGE:` footer a major release. The messages
   decide the next version number.
4. A pull request against `main` says what changed and why.

## How the code is written

- A doc comment follows the godoc style. It starts with the name that it documents, uses whole sentences and wraps at
  120 columns. Comments inside functions are rare and say why, not what.
- States and settings are bitwise flags, not strings or booleans. Counters are atomic. The code uses no mutexes.
- The code uses standard interfaces such as `io.Writer` where it can.
- The path of a packet must not allocate memory more than the tests allow. `testing.AllocsPerRun` checks it.
- Everything that arrives from the network is hostile. Sizes are limited, values are checked before they are used, and
  what is dropped is counted.
- A composite literal gives each field a line of its own. In a test, this holds for literals of keyed fields, so that
  tables of test data stay compact. `make check-style` finds the literals that break it, and `make fix-style` rewrites
  them.
- Every error is declared in the `errors.go` of its package, each with its own `var`. `make check-style` checks it.
- Each package-level `var` stands on its own, never in a `var ( ... )` group.
- The linters are the rules of the project. A `//nolint` comment silences none of them. The code changes, or
  `.golangci.yml` changes in a pull request of its own.

## Git hooks

`make hooks` installs the hooks of `lefthook.yml`. Before a commit, they run `gofmt`, `make check-style`, `make lint`,
`shellcheck` and `prettier` on the staged files, in parallel. They also check that the first line of the commit message
starts with a type, such as `feat(api): ...`, because semantic-release reads it. `lefthook` is built from the `tools`
module, so nothing else needs to be installed. The workflow runs the same checks again, so a hook that was skipped with
`--no-verify` is found there.

## The workflow

The workflow in `.github/workflows/ci.yml` runs on every pull request and on `main`. It builds and deploys nothing.

| Job              | Command           | What it checks                                                           |
| ---------------- | ----------------- | ------------------------------------------------------------------------ |
| `fmt-vet`        | `make fmt vet`    | `gofmt` and `go vet`.                                                    |
| `test-race`      | `make test-race`  | The Go tests with the race detector.                                     |
| `lint`           | `make lint`       | `golangci-lint`, with the rules in `.golangci.yml`.                      |
| `shellcheck`     | `make shellcheck` | `shellcheck` on the scripts.                                             |
| `style`          | `make check-style` | A line for each field of a literal, no `var` groups, and every error in `errors.go`.      |
| `staticcheck`    | `make staticcheck` | `staticcheck`.                                                          |
| `vulncheck`      | `make vulncheck`  | `govulncheck`: known vulnerabilities in the code that the backend calls. |
| `frontend-check` | `make frontend-check frontend-build` | Type check, unit tests, Prettier and the build of the page. |
| `release`        | `semantic-release` | On `main` only. See below.                                              |

`golangci-lint`, `staticcheck`, `lefthook` and the project's own checks (`apps/backend/tools/check`, analyzers that run
as one program) are in a separate Go module, `apps/backend/tools`, so they do not touch the `go.sum` of the backend. The
Makefile builds or runs them from there. An update of one is `go get -modfile=tools/go.mod <module>@<version>` in
`apps/backend` for `golangci-lint` and `staticcheck`, or `go get <module>@<version>` in `apps/backend/tools` for the
others.

## Releases

Merging to `main` is all it takes. The `release` job runs semantic-release. It writes the notes of the release into
`CHANGELOG.md`, commits that file to `main` as `chore(release): X.Y.Z`, creates the `vX.Y.Z` tag on that commit, and
creates a GitHub Release with the same notes. The release commit starts no workflow of its own. Nothing is deployed and
no image is published.

The job uses the workflow's own `GITHUB_TOKEN`, which may push the commit and create the release. `CHANGELOG.md` is
never edited by hand. The commit messages generate it.

`.releaserc.yml` names the plugins in the order that they run: the commit analyzer decides the version, the notes
generator writes the notes, the changelog plugin puts them in `CHANGELOG.md`, the git plugin commits that file, and the
GitHub plugin makes the release. `release/package.json` pins semantic-release and every plugin to an exact version, and
`release/package-lock.json` locks the rest. A version bump changes it in `release/package.json`, runs
`npm install --prefix release --package-lock-only`, and commits both files.

## Pinned container images

Every base image is pinned to an exact digest, not only to a tag. The images of the Docker build are in
`scripts/versions.env`. An update of an image takes the tag and the digest from `docker buildx imagetools inspect <image>:<tag>` and replaces
both together. The actions of the workflow in `.github/workflows/ci.yml` are pinned to a commit the same way.

## Licensing

Every contribution is published under the MIT License in `LICENSE`.

The licences of the libraries that the page and the server carry are in
`apps/frontend/public/third-party-licenses.txt`, and the page serves the file. A change to `go.mod` or to
`apps/frontend/package-lock.json` is followed by `make licenses`, and the result is committed with it.
