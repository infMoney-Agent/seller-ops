package main

import (
	"testing"
	"time"
)

func TestImportDateDefaults(t *testing.T) {
	for _, c := range []struct{ name, want string }{
		{"Product-Beauty&PersonalCare-US-2026.08-430575.xlsx", "2026-08"},
		{"Product-US-2026-07-export.csv", "2026-07"},
		{"Product-US-2026_09-export.zip", "2026-09"},
		{"Product-US-2026.13-export.xlsx", ""},
		{"Product-2026.07-to-2026.08.xlsx", ""},
		{"Products.xlsx", ""},
	} {
		if got := sourcePeriodFromFilename(c.name); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
	now := time.Date(2026, 11, 18, 17, 0, 0, 0, time.UTC)
	values := Object{}
	applyImportDefaults(values, "Product-US-2026.08-430575.xlsx", now)
	if values["source_period"] != "2026-08" || values["observed_on"] != "2026-11-19" {
		t.Fatal(values)
	}
	explicit := Object{"source_period": "2025-01", "observed_on": "2025-02-03"}
	applyImportDefaults(explicit, "Product-US-2026.08.xlsx", now)
	if explicit["source_period"] != "2025-01" || explicit["observed_on"] != "2025-02-03" {
		t.Fatal("manual values overwritten", explicit)
	}
	unknown := Object{}
	applyImportDefaults(unknown, "Products.xlsx", now)
	meta, err := period(unknown, false)
	if err != nil || meta["period_start"] != nil || str(meta["source_period"]) != "" {
		t.Fatal("unknown month was guessed from today", meta, err)
	}
	if _, err = period(unknown, true); err == nil {
		t.Fatal("unidentified source month accepted for commit")
	}
}
