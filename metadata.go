package vat

import (
	"context"
	"time"
)

type VatOpMetadata struct {
	Version string
	Date    time.Time
}

type VatMetadata struct {
	SaveData *VatOpMetadata
	LoadData *VatOpMetadata
}

func (v *VatOpMetadata) serialize() map[string]string {
	r := make(map[string]string, 2)
	r["version"] = v.Version
	r["date"] = v.Date.Format(time.RFC3339)
	return r
}

func (v *VatMetadata) Serialize() map[string]string {
	r := make(map[string]string, 4)
	if v.SaveData != nil {
		for k, v := range v.SaveData.serialize() {
			r["save-"+k] = v
		}
	}
	if v.LoadData != nil {
		for k, v := range v.LoadData.serialize() {
			r["load-"+k] = v
		}
	}
	return r
}

func NewVatOpMetadata(ctx context.Context) *VatOpMetadata {
	var version string = "none_found"
	if ctx.Value(VERSION) != nil {
		version = string(ctx.Value(VERSION).(VersionNumber))
	}
	return &VatOpMetadata{
		Version: version,
		Date:    time.Now(),
	}
}
