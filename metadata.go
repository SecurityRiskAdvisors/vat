package vat

import (
	"context"
	"time"
)

type VatOpMetadata struct {
	Version      string
	Date         time.Time
	VectrVersion string
}

type VatMetadata struct {
	SaveData *VatOpMetadata
	LoadData *VatOpMetadata
}

/*
serialize converts VatOpMetadata into a map of strings.

This method includes version, date, and VECTR version.
It formats the date using the RFC3339 standard.

Returns:
  - A map of strings representing serialized operation metadata.
*/
func (v *VatOpMetadata) serialize() map[string]string {
	r := make(map[string]string, 2)
	r["version"] = v.Version
	r["date"] = v.Date.Format(time.RFC3339)
	r["vectr-version"] = v.VectrVersion
	for k, _ := range r {
		if r[k] == "" {
			r[k] = "none_found"
		}
	}
	return r
}

/*
Serialize converts VatMetadata into a map of strings.

This method includes save and load operation metadata.
It prefixes keys with "vat-save-" or "vat-load-" based on the operation type.

Returns:
  - A map of strings representing serialized metadata.
*/
func (v *VatMetadata) Serialize() map[string]string {
	r := make(map[string]string, 4)
	if v.SaveData != nil {
		for k, v := range v.SaveData.serialize() {
			r["vat-save-"+k] = v
		}
	}
	if v.LoadData != nil {
		for k, v := range v.LoadData.serialize() {
			r["vat-load-"+k] = v
		}
	}
	return r
}

/*
NewVatOpMetadata creates a new VatOpMetadata instance using context values.

This function extracts the version and VECTR version from the provided context.
If these values are not present, it defaults to "none_found".
The current date is set using the time of creation.

Parameters:
  - ctx: Context for managing request lifetimes and cancellations.

Returns:
  - A pointer to a VatOpMetadata struct containing:
  - Version: The version extracted from the context.
  - Date: The current date and time.
  - VectrVersion: The VECTR version extracted from the context.
*/
func NewVatOpMetadata(ctx context.Context) *VatOpMetadata {
	var version string = "none_found"
	var vectrVersion string = "none_found"
	if ctx.Value(VERSION) != nil {
		version = string(ctx.Value(VERSION).(VatContextValue))
	}
	if ctx.Value(VECTR_VERSION) != nil {
		vectrVersion = string(ctx.Value(VECTR_VERSION).(VatContextValue))
	}
	return &VatOpMetadata{
		Version:      version,
		Date:         time.Now(),
		VectrVersion: vectrVersion,
	}
}
