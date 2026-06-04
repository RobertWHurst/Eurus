package natstransport

import "testing"

func TestFormatForNamespace(t *testing.T) {
	cases := map[string]string{
		"":            "",
		"simple":      "simple",
		"camelCase":   "camel-case",
		"HTTPServer":  "HTTPServer", // only lower->upper boundaries split
		"myHTTPThing": "my-hTTPThing",
		"with_under":  "with-under",
		"with-dash":   "with-dash",
		"with.dot":    "with.dot",
		"star*":       "star*",
		"a1B2":        "a1B2",
		"drop/slash":  "dropslash", // unsupported chars are dropped
		"socketABC":   "socket-aBC",
	}
	for in, want := range cases {
		if got := formatForNamespace(in); got != want {
			t.Errorf("formatForNamespace(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNamespace(t *testing.T) {
	if got := namespace("service", "myID", "message"); got != "eurus.service.my-iD.message" {
		t.Errorf("unexpected namespace: %q", got)
	}
	// Empty chunks are skipped.
	if got := namespace("gateway", "", "message"); got != "eurus.gateway.message" {
		t.Errorf("unexpected namespace with empty chunk: %q", got)
	}
}
