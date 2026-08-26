package api

import (
	"testing"

	"purpcmd/pkg/teamapi"
)

func TestEventReplayPageHasGap(t *testing.T) {
	tests := []struct {
		name    string
		after   uint64
		records []teamapi.EventRecord
		want    bool
	}{
		{name: "empty", after: 7},
		{name: "contiguous", after: 7, records: []teamapi.EventRecord{{Sequence: 8}, {Sequence: 9}}},
		{name: "missing first", after: 7, records: []teamapi.EventRecord{{Sequence: 9}}, want: true},
		{name: "missing middle", after: 7, records: []teamapi.EventRecord{{Sequence: 8}, {Sequence: 10}}, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := eventReplayPageHasGap(test.after, test.records); got != test.want {
				t.Fatalf("eventReplayPageHasGap(%d, %#v) = %t, want %t", test.after, test.records, got, test.want)
			}
		})
	}
}
