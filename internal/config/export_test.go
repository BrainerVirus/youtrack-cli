package config

import "testing"

// asOS makes path globs compare as on goos for the rest of the test.
func asOS(t *testing.T, goos string) {
	t.Helper()
	t.Cleanup(SetPathOS(goos))
}
