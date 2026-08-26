# Fork and release policy

This repository is an independently versioned public fork of
[`speedata/einvoice`](https://github.com/speedata/einvoice). The upstream Git
history, BSD-3-Clause license, and notices are retained.

## Upstream synchronization

- `upstream` points to `speedata/einvoice`; updates are merged, never squashed.
- Fork-specific commits stay small and are proposed upstream whenever they are
  generally useful.
- Every sync records the upstream commit in the pull request and reruns the
  complete conformance, race, fuzz, and differential-parity gates.
- An upstream sync never bypasses the fork's semantic-versioning policy.

## Releases

- The module path is `github.com/jxsl13/einvoice`.
- Manual feature releases use signed semantic-version tags. Automated
  dependency-only patch releases originate from protected main and publish
  checksums plus GitHub provenance attestations. Production candidates also
  publish an SPDX SBOM, test coverage, rule-inventory coverage, corpus
  provenance, and KoSIT parity.
- Pre-1.0 releases may change the API only through documented release notes.
- Hugo Studio pins an exact released version and checksum behind a narrow
  adapter; it never consumes an untagged branch or pseudo-version.
- A release is not approved for fiscal issuance unless its conformance report
  shows every enabled official rule ID, at least 95% Go statement coverage,
  at least 99.9% normalized KoSIT verdict parity, and zero false accepts for
  fatal or error findings.

## Dependency train

- Dependabot opens grouped weekly Go-module and GitHub Actions updates.
- `.github/dependencies/parity.json` is the explicit non-shipping toolchain
  dependency manifest for KoSIT Validator, XRechnung configuration, and
  XRechnung Schematron. A weekly job resolves releases and downloads every asset
  into a disposable directory to verify the advertised SHA-256 before proposing
  a change.
- Dependency pull requests are armed for squash auto-merge, but protected main
  merges them only after every required CI check passes.
- Each merged dependency train reruns the release gate and increments only the
  patch component. The release is blocked if any commit since the latest release
  is not associated with a dependency-labeled pull request, so feature, minor,
  and major releases stay manual.
- The hourly release reconciler catches a bot-created pull request even when a
  GitHub token suppresses recursive workflow events.

## Java boundary

Production code and all release assets contain zero Java source, bytecode,
archives, runtimes, build tools, or commands. A pinned KoSIT Java validator may
be used only by a separately isolated, non-shipping differential-test job. Its
inputs and normalized outputs are test evidence, not module dependencies.
