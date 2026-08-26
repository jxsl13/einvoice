# Conformance status

The stable native-Go rule pack `xrechnung-3.0.2-2026-01-31` validates standard
XRechnung 3.0.2 invoices in UBL 2.1 Invoice, UBL 2.1 CreditNote, and UN/CEFACT
CII D16B syntax. It embeds the required XML schemas and performs no filesystem
or network resolution at runtime.

Extension and CVD customization identifiers are deliberately rejected as
unsupported. The legacy `bootstrap-0` rule pack remains available for
compatibility and is not a conformance claim.

## Reproducible evidence

The complete checksum-pinned toolchain is recorded in
`.github/dependencies/parity.json`. Java is used only as an ephemeral,
network-isolated differential oracle in CI. No Java source, bytecode, archive,
runtime, or container layer is part of this Go module or its release assets.

The checked-in report at `conformance/xrechnung-3.0.2-report.json` records:

- 451 of 451 supported documents with matching acceptance verdicts;
- 342 of 342 matching fatal/error rule IDs;
- zero false accepts;
- 67 Extension/CVD documents skipped as explicitly unsupported;
- 71 CII and 76 UBL predicates in the immutable official rule inventory.

CI regenerates the rule inventory, recreates the XML Mutate witnesses, runs the
pinned KoSIT oracle, and reruns the native-Go comparison. It requires at least
99.9% verdict and fatal/error finding parity and permits no false accepts.

## Blocking release gates

- every enabled standard/PEPPOL predicate mapped for both claimed syntaxes;
- at least 99.9% normalized KoSIT verdict and fatal/error rule-ID parity;
- zero native-Go false accepts where KoSIT rejects;
- at least 90% statement coverage in every production package;
- race tests, fuzz smoke tests, static analysis, vulnerability scanning, and
  checksum verification;
- no runtime external resolution and no Java artifacts in module or release;
- signed semantic-version tag, source archive checksums, SPDX SBOM, and
  published conformance evidence.

Passing validation is technical evidence against the pinned rule set, not tax
or legal advice. Invoice issuers remain responsible for the underlying facts
and all applicable retention, authenticity, integrity, and tax obligations.
