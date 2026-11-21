package api

import (
	"bytes"
	"testing"
)

func TestHintFields(t *testing.T) {
	tests := []struct {
		name, query, body string
		hint              bool
	}{
		{"given an object with only $type, it hints --fields", "", `{"$type":"Me"}`, true},
		{"given a list of objects with only $type and id, it hints --fields", "$top=2", `[{"$type":"Issue","id":"2-1"},{"$type":"Issue","id":"2-2"}]`, true},
		{"given fields= in the query, it stays quiet", "fields=id", `{"$type":"Me"}`, false},
		{"given attributes beyond $type and id, it stays quiet", "", `{"$type":"Me","login":"jdoe"}`, false},
		{"given an empty list, it stays quiet", "", `[]`, false},
		{"given a non-object value, it stays quiet", "", `42`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var w bytes.Buffer
			hintFields(&w, tt.query, []byte(tt.body))
			if got := w.Len() > 0; got != tt.hint {
				t.Errorf("hint = %q, want hint %v", w.String(), tt.hint)
			}
		})
	}
}
