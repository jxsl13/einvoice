# Conformance roadmap

This fork is not yet approved as an authoritative fiscal-issuance validator.
Its purpose is to reach reproducible native-Go parity with the official German
XRechnung validation configuration while keeping all release artifacts and
production consumers Java-free.

## Pinned oracle baseline

The machine-readable source of truth is
[`.github/dependencies/parity.json`](.github/dependencies/parity.json). A weekly
dependency job verifies the latest three release assets by SHA-256 and opens a
protected auto-merge pull request whenever a pin changes.

| Artifact | Version | SHA-256 |
| --- | --- | --- |
| [KoSIT Validator](https://github.com/itplr-kosit/validator/releases/tag/v1.6.3) | 1.6.3 standalone | `799e64befca97d4080e03608c80b85dd5a5ecc5f4ae4f35d1116ec2855b9a7c9` |
| [XRechnung configuration](https://github.com/itplr-kosit/validator-configuration-xrechnung/releases/tag/v2026-01-31) | XRechnung 3.0.2, 2026-01-31 | `6a5a5911a421b25fbc423f62f93f894df7b236f5d73ca4f84bb222a945082704` |
| [XRechnung Schematron](https://github.com/itplr-kosit/xrechnung-schematron/releases/tag/v2.5.0) | 2.5.0 | `a0f3d82737759bee8591c298ff24983a8f1c667f85e45a34863c75a242bc6f43` |

The validator and configuration run only as an isolated differential-test
oracle. They are downloaded by checksum into an ephemeral job and are absent
from this Go module, module archives, release assets, and consuming systems.

## Measured baseline

At fork creation from upstream commit
`4042b0c17014239cb9da1e1042f5b9951dc1d49a`:

- the root package passed race-enabled tests at 93.7% statement coverage;
- the complete module reported 81.2% because command and generator packages are
  largely untested;
- the code declared 348 rule IDs;
- exact ID inventory covered 34 of 37 standard CII XRechnung IDs and 33 of 34
  standard UBL IDs from Schematron 2.5.0;
- the explicit standard gaps were CII BR-TMP-4, BR-TMP-5, BR-TMP-7 and UBL
  BR-TMP-6;
- ID presence is not semantic parity. The current IBAN helper, for example,
  checks structure but not the official modulo-97 condition.

## Production-candidate gates

Every gate is blocking:

- 100% of enabled official rule IDs mapped for every claimed syntax/profile;
- at least one passing and one single-condition failing witness per rule and
  applicable syntax;
- at least 95% statement coverage in production packages, with no package below
  90%;
- 100% true/false outcome coverage for each enabled predicate;
- at least 90% mutation score in rule and normalization packages;
- at least 99.9% normalized KoSIT acceptance and finding parity;
- zero native-Go false accepts where KoSIT reports fatal/error rejection;
- zero untriaged differential mismatches, races, panics, external resolution,
  unbounded inputs, or prohibited artifacts;
- signed semantic-version tag, checksums, SPDX SBOM, corpus provenance,
  conformance JSON, license scan, and vulnerability scan.

The current 90% CI floor is a bootstrap floor, not the production threshold.

## Current implementation progress

The project now exposes a `validator` package with a context-aware API, typed
operational errors, deterministic localization-neutral findings, an explicitly
non-authoritative `bootstrap-0` rule pack, and pre-parse ceilings for bytes,
depth, element count, attributes, text nodes, and returned findings. It rejects
DTD directives, non-declaration processing instructions, XInclude, unknown
roots, duplicate profile declarations, unsupported profiles, and unsupported
rule packs before business-rule validation. Hostile-input tests, race tests,
and a dedicated fuzz target cover this boundary.

This completes only the API and initial resource-boundary portion of the
roadmap. It does not satisfy rule inventory, witness, mutation, differential
parity, false-accept, provenance, supply-chain, or adviser-acceptance gates and
does not change the module's fiscal-conformance status.

## Implementation sequence

1. Add a bounded context-aware validation API and deterministic typed findings.
2. Generate a reviewed rule inventory from pinned official artifacts.
3. Harden CII/UBL parsing and structural validation without external resolution.
4. Close standard XRechnung ID and semantic gaps.
5. Build licensed positive, negative, mutation, and malformed-input corpora.
6. Normalize KoSIT reports and run deterministic differential testing.
7. Reach coverage, mutation, fuzzing, security, and supply-chain gates.
8. Publish a signed pre-release with reproducible conformance evidence.

Extension and CVD profiles must either reach the same complete gates or be
rejected explicitly as unsupported. Partial profile claims are prohibited.
