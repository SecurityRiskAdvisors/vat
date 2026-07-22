package vat

import (
	"context"
	"time"
)

// versionsFromContext extracts the vat/VECTR versions stamped on ctx (see
// const.go's VERSION/VECTR_VERSION keys), defaulting to "none_found" if
// either is absent.
func versionsFromContext(ctx context.Context) (version, vectrVersion string) {
	version = "none_found"
	vectrVersion = "none_found"
	if ctx.Value(VERSION) != nil {
		version = string(ctx.Value(VERSION).(VatContextValue))
	}
	if ctx.Value(VECTR_VERSION) != nil {
		vectrVersion = string(ctx.Value(VECTR_VERSION).(VatContextValue))
	}
	return version, vectrVersion
}

// orDefault returns s, or def if s is empty.
func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// VatOpMetadata captures provenance for a single restore operation: what vat
// version is running, when, and against what VECTR version. It's an
// artifact of a single RestoreAssessment/RestoreCampaign call — computed as
// a local variable there (not stored on AssessmentData, see its doc
// comment) and never part of the wire file. Save-time provenance lives
// directly in Manifest (format.go) instead, since that's the file's own
// record of who saved it, not a separate shadow copy of it.
type VatOpMetadata struct {
	Version      string
	Date         time.Time
	VectrVersion string
}

// NewVatOpMetadata builds a VatOpMetadata stamped with the current time and
// the vat/VECTR versions carried on ctx.
func NewVatOpMetadata(ctx context.Context) VatOpMetadata {
	version, vectrVersion := versionsFromContext(ctx)
	return VatOpMetadata{
		Version:      version,
		Date:         time.Now(),
		VectrVersion: vectrVersion,
	}
}

// asMap renders v as the unprefixed {version, date, vectr-version} shape
// used for display (see diag.go).
func (v VatOpMetadata) asMap() map[string]string {
	date := ""
	if !v.Date.IsZero() {
		date = v.Date.Format(time.RFC3339)
	}
	return map[string]string{
		"version":       orDefault(v.Version, "none_found"),
		"date":          orDefault(date, "none_found"),
		"vectr-version": orDefault(v.VectrVersion, "none_found"),
	}
}

// asPrefixedMap renders v's fields prefixed (e.g. "vat-load-version"), for
// writing into VECTR's own generic metadata key/value pairs.
func (v VatOpMetadata) asPrefixedMap(prefix string) map[string]string {
	r := make(map[string]string, 3)
	for k, val := range v.asMap() {
		r[prefix+"-"+k] = val
	}
	return r
}

// AsVectrMetadataPairs flattens save-time provenance (manifest) and
// load-time provenance (restoreInfo) into VECTR's generic metadata
// key/value pairs, prefixed "vat-save-*" / "vat-load-*" respectively. This
// is restore-time bookkeeping written into the target VECTR instance for
// audit purposes — it has nothing to do with vat's own wire format.
func AsVectrMetadataPairs(manifest Manifest, restoreInfo VatOpMetadata) map[string]string {
	r := manifest.asPrefixedMap("vat-save")
	for k, v := range restoreInfo.asPrefixedMap("vat-load") {
		r[k] = v
	}
	return r
}
