package vat

// This file is the only place where the supported VECTR version range
// differs between vat 1.x and vat 2.x. Keep everything else in the version
// check byte-identical across branches so vat 2.0 rebases cleanly.

// vat 1.x supports VECTR below 9.14; VECTR 9.14 and later require vat 2.x.
var supportedVectrRange = vectrVersionRange{Max: "9.14"}

// versionCheckAdvice is appended to the error when the check fails.
const versionCheckAdvice = "VECTR 9.14 and later require vat 2.x; use --ignore-version-check to proceed anyway"
