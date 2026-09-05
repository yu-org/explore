package model

import "testing"

func TestDecodeEventValue(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want string
	}{
		{"empty", nil, ""},
		{"text", []byte("Transfer Completed!"), "Transfer Completed!"},
		{"json", []byte(`{"ok":true}`), `{"ok":true}`},
		{"binary", []byte{0x00, 0x01, 0xff}, "0x0001ff"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DecodeEventValue(tt.in); got != tt.want {
				t.Errorf("DecodeEventValue() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEventsJSONRoundTrip(t *testing.T) {
	events := []string{"one", "two \"quoted\""}
	got := EventsFromJSON(EventsJSON(events))
	if len(got) != len(events) {
		t.Fatalf("got %d events, want %d", len(got), len(events))
	}
	for i := range events {
		if got[i] != events[i] {
			t.Errorf("event %d = %q, want %q", i, got[i], events[i])
		}
	}
	if EventsJSON(nil) != "" {
		t.Error("EventsJSON(nil) should be empty")
	}
	if EventsFromJSON("") != nil {
		t.Error("EventsFromJSON(\"\") should be nil")
	}
}
