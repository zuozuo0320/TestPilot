package api

import "testing"

func TestNormalizeGeneratedStepsForTestCase(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "json array steps",
			raw:  `[{"action":"Open dashboard","expected":"Dashboard is visible"},{"action":"Click sync","expected":"Sync succeeds"}]`,
			want: "Open dashboard | Dashboard is visible\nClick sync | Sync succeeds",
		},
		{
			name: "plain text steps",
			raw:  "  Open dashboard | Dashboard is visible  ",
			want: "Open dashboard | Dashboard is visible",
		},
		{
			name: "skip empty rows",
			raw:  `[{"action":"","expected":""},{"action":"Submit form","expected":""}]`,
			want: "Submit form | ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeGeneratedStepsForTestCase(tt.raw); got != tt.want {
				t.Fatalf("normalizeGeneratedStepsForTestCase() = %q, want %q", got, tt.want)
			}
		})
	}
}
