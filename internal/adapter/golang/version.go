package golang

// ModelVersion identifies what the reader writes into a domain.PackageModel
// for a given source tree. Stored models are keyed by it, so they survive a
// rebuild of the binary and are discarded only when this number moves.
//
// Bump it whenever identical source would parse to a different model with the
// struct shape unchanged: a new or fixed extraction rule, a changed
// stereotype heuristic, a different span, path or ordering convention, an
// upgrade of go/packages that changes what is read. A changed shape of
// domain.PackageModel needs no bump; the model store fingerprints the shape
// itself. A change left unbumped is served from the models the previous
// reader stored, and reads as a fix that did not take effect.
const ModelVersion = 1
