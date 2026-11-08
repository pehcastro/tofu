package incr

// PruningCertificateVersion identifies the certificate used by incremental
// recovery admission. Advance it when certificate v2 replaces the legacy
// proof (universal-core PR 1.2); the temporary W5 exemptions expire then.
const PruningCertificateVersion = 1
