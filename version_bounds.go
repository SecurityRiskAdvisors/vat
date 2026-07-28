package vat

// This file is the only place where the supported VECTR version range
// differs between vat 1.x and vat 2.x. Keep everything else in the version
// check byte-identical across branches so vat 2.0 rebases cleanly.

// vat 2.x supports VECTR 9.14 and later; earlier VECTR versions require vat 1.x.
var supportedVectrRange = vectrVersionRange{Min: "9.14"}

// versionCheckAdvice is appended to the error when the check fails.
const versionCheckAdvice = "VECTR versions before 9.14 require vat 1.x; use --ignore-version-check to proceed anyway"
