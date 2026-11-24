# Versioned protocols

Cross-language worker contracts live under a versioned namespace such as
`tacit.runtime.v1`. A protocol change must be backward compatible within a
version or introduce a new version.

`runtime/v1` defines the broker event envelope and the closed worker-result
union. The Go control plane validates the corresponding domain contracts before
persistence. Executors may generate language-specific models from these source
schemas in CI; generated client artifacts are not repository source.

TAC-026 will add immutable media manifests without putting media bytes into
these control-plane messages.
