package validate

import "testing"

// These fuzz targets assert the compose port parsers never panic on
// author-supplied input. The seed corpus also runs as a normal regression
// test in CI; deep runs are opt-in via `go test -fuzz=...`.

func FuzzSplitPortString(f *testing.F) {
	for _, s := range []string{
		"14000:8000", "8000", "${PORT_8000:-8000}:8000",
		"127.0.0.1:14000:8000", "", ":", "::", ":8000", "8000:",
		"abc:def", "   : ", "-1:-1", "99999999999999999999:1",
		"[::1]:8000:80", "${PORT}", ":::",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		host, _ := splitPortString(s)
		// Exercise the full chain a caller would use.
		_, _ = parseHostPort(host)
	})
}

func FuzzComposePortUnmarshal(f *testing.F) {
	for _, s := range []string{
		`"14000:8000"`, `"8000"`, `"${PORT_8000:-8000}:8000"`,
		`{"published":14000,"target":8000}`,
		`{"published":"${PORT}","target":8000}`,
		`{"published":true,"target":0}`,
		`123`, `null`, `[]`, `""`, `"::"`, `{}`, `{"target":-1}`,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		var p composePort
		// Arbitrary bytes: an error is fine, a panic is not.
		_ = p.UnmarshalJSON(b)
	})
}
