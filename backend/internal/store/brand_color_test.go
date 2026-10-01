package store

import "testing"

// The colour lands inside a style attribute in every email; anything but a hex colour could break out of it.
func TestOnlyAHexColourIsABrandColour(t *testing.T) {
	for _, ok := range []string{"#C9A227", "#fff", "#0a0a0a"} {
		if !ValidBrandColor(ok) {
			t.Errorf("%q refused", ok)
		}
	}
	for _, bad := range []string{"", "red", "#12345", "#C9A227;", `#fff"><a href=x>`, "rgb(1,2,3)"} {
		if ValidBrandColor(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
	s := TenantSettings{PrimaryColor: `#fff"><script>`}
	if got := s.SafePrimaryColor(); got != DefaultTenantSettings.PrimaryColor {
		t.Errorf("SafePrimaryColor() = %q, want the default", got)
	}
}
