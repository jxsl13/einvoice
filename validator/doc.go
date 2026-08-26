// Package validator provides the bounded, context-aware validation API.
//
// The package is deliberately fail closed. It accepts only explicitly known
// syntaxes, profiles, and rule packs; applies hard XML resource ceilings before
// the legacy parser builds an in-memory tree; and returns localization-neutral
// findings. The bootstrap rule pack is not a claim of fiscal conformance. See
// CONFORMANCE.md in the module repository for the production-candidate gates.
package validator
