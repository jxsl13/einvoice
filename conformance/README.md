# Conformance evidence

`xrechnung-3.0.2-rules.json` is the deterministic inventory generated from the
checksum-pinned XRechnung Schematron 2.5.0 sources. It binds each predicate to
its syntax, capability group, severity, normalized expression, and SHA-256.
Standard, PEPPOL, and infrastructure groups are enabled by the stable rule
pack; Extension and CVD remain explicitly unsupported.

`xrechnung-3.0.2-report.json` records the differential KoSIT result, corpus
sizes, false-accept count, embedded-schema digest, and release thresholds.

Regenerate and verify the inventory with the exact archive pinned in
`.github/dependencies/parity.json`:

```console
go run ./cmd/genruleinventory -archive /path/to/xrechnung-schematron.zip -verify
```

CI additionally recreates the official mutation witnesses and compares all
supported documents with the pinned KoSIT oracle. Oracle artifacts are
ephemeral and never ship in the module or release.
