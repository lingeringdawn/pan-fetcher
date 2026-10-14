package jackett

import (
	"strings"
	"testing"
)

// TestCheckTorznabBody covers the discriminator that a status code alone cannot
// provide: Jackett reports a rejected API key as HTTP 200 with an <error>
// document, so the body is the only reliable signal.
func TestCheckTorznabBody(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantErr bool
		errText string
	}{
		{
			name:    "caps document is healthy",
			body:    `<?xml version="1.0" encoding="UTF-8"?>` + "\n" + `<caps><server title="Jackett" /><limits default="1000" max="1000" /></caps>`,
			wantErr: false,
		},
		{
			name:    "indexers document is healthy",
			body:    `<?xml version="1.0" encoding="UTF-8"?>` + "\n" + `<indexers><indexer id="x"><title>X</title></indexer></indexers>`,
			wantErr: false,
		},
		{
			name:    "empty indexers list is healthy",
			body:    `<?xml version="1.0" encoding="UTF-8"?>` + "\n" + `<indexers />`,
			wantErr: false,
		},
		{
			name:    "invalid api key is an error",
			body:    `<?xml version="1.0" encoding="UTF-8"?>` + "\n" + `<error code="100" description="Invalid API Key" />`,
			wantErr: true,
			errText: "API Key",
		},
		{
			name:    "error without xml declaration",
			body:    `<error code="100" description="Invalid API Key" />`,
			wantErr: true,
			errText: "API Key",
		},
		{
			name:    "other error code is reported with its description",
			body:    `<?xml version="1.0" encoding="UTF-8"?>` + "\n" + `<error code="201" description="Indexer is not configured" />`,
			wantErr: true,
			errText: "Indexer is not configured",
		},
		{
			name:    "error with no description still fails",
			body:    `<error code="999" />`,
			wantErr: true,
		},
		{
			name:    "leading whitespace is tolerated",
			body:    "\n\n   " + `<?xml version="1.0"?>` + "\n" + `  <error code="100" description="Invalid API Key" />`,
			wantErr: true,
			errText: "API Key",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkTorznabBody([]byte(tc.body))
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				if tc.errText != "" && !strings.Contains(err.Error(), tc.errText) {
					t.Errorf("error %q does not mention %q", err.Error(), tc.errText)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// TestCheckTorznabBodyRFC7807 verifies the helper is not confused by a JSON body
// (Prowlarr can answer with JSON errors); it must simply not report a Torznab
// error for content it does not recognise.
func TestCheckTorznabBodyJSON(t *testing.T) {
	if err := checkTorznabBody([]byte(`{"error":"Invalid API key"}`)); err != nil {
		t.Errorf("JSON body should not be treated as a Torznab error document: %v", err)
	}
}
