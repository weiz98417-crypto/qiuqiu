package deliverykey

import (
	"testing"

	"qiuqiu/internal/matchstate"
)

func TestForEventMatchesMatchStateFormat(t *testing.T) {
	event := matchstate.MatchEvent{ID: "ev1", FactRevision: 3, FactStatus: "confirmed"}
	if got, want := ForEvent(event), "ev1:3:confirmed"; got != want {
		t.Fatalf("ForEvent = %q, want %q", got, want)
	}
}

func TestKeyFormats(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"trace", ForTrace("t1"), "t1"},
		{"reminder", ForReminder("r1"), "reminder:r1"},
		{"backchannel", ForBackchannel("e9"), "backchannel-e9"},
		{"observation", ForObservation("p1", 4, "resolved"), "p1:4:resolved"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s key = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}
