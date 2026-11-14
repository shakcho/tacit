# Versioned protocols

Cross-language worker contracts live under a versioned namespace such as `tacit.runtime.v1`. A protocol change must be backward compatible within a version or introduce a new version. Generated Go, TypeScript and Python artifacts are committed and checked for drift.

No protocol exists yet: TAC-009 owns the first durable job/result envelope and TAC-026 owns media manifests.
