// Package validator provides the bounded, context-aware validation API.
//
// The package is deliberately fail closed. It accepts only explicitly known
// syntaxes, profiles, and rule packs; applies hard XML resource ceilings before
// the legacy parser builds an in-memory tree; and returns localization-neutral
// findings. RulePackXRechnung302 is the stable, checksum-pinned XRechnung 3.0.2
// pack; the bootstrap pack remains non-authoritative compatibility behavior.
// See CONFORMANCE.md in the module repository for evidence and scope.
package validator
