package customfields

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func definition(t *testing.T, wire string) Definition {
	t.Helper()
	var d Definition
	if err := json.Unmarshal([]byte(wire), &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestDefinitionDecode(t *testing.T) {
	t.Run("given a multi-value bundle field, it keeps the kind, multiplicity and usable values", func(t *testing.T) {
		d := definition(t, `{"$type":"VersionProjectCustomField","canBeEmpty":true,"field":{"name":"Fix versions","fieldType":{"id":"version[*]"}},
			"bundle":{"values":[{"name":"2026.2","archived":true},{"name":"2026.3","archived":false}]}}`)
		if d.Name != "Fix versions" || d.Kind != KindVersion || !d.Multi || !d.CanBeEmpty ||
			strings.Join(d.Values, ",") != "2026.3" || strings.Join(d.Archived, ",") != "2026.2" {
			t.Errorf("got %+v", d)
		}
	})

	t.Run("given a field type ytrack does not know, it is unknown and cannot be encoded", func(t *testing.T) {
		d := definition(t, `{"field":{"name":"Mood","fieldType":{"id":"hologram[1]"}}}`)
		if d.Kind != KindUnknown {
			t.Fatalf("kind = %s", d.Kind)
		}
		if _, err := d.Encode("happy", EncodeOptions{}); err == nil || !strings.Contains(err.Error(), "use `ytrack issue command`") {
			t.Errorf("err = %v", err)
		}
	})
}

func TestEncode(t *testing.T) {
	zone := time.FixedZone("UTC+2", 2*60*60)
	opts := EncodeOptions{Location: zone, Me: func() (string, error) { return "jdoe", nil }}
	tests := []struct {
		name, fieldType, raw string
		users                []User
		want                 string // JSON of Field, or "command:<text>"
	}{
		{"a state ignores case", "state[1]", "in progress", nil, `{"$type":"StateIssueCustomField","name":"F","value":{"name":"In Progress"}}`},
		{"a multi enum splits on commas", "enum[*]", "Bug, in progress", nil, `{"$type":"MultiEnumIssueCustomField","name":"F","value":[{"name":"Bug"},{"name":"In Progress"}]}`},
		{"a user resolves me", "user[1]", "me", []User{{Login: "jroe"}}, `{"$type":"SingleUserIssueCustomField","name":"F","value":{"login":"jdoe"}}`},
		{"a user login ignores case", "user[*]", "JROE", []User{{Login: "jroe"}}, `{"$type":"MultiUserIssueCustomField","name":"F","value":[{"login":"jroe"}]}`},
		{"a date is midday UTC", "date", "2026-10-07", nil, `{"$type":"DateIssueCustomField","name":"F","value":1791374400000}`},
		{"a local date-time uses the zone", "date and time", "2026-10-07T14:00", nil, `{"$type":"SimpleIssueCustomField","name":"F","value":1791374400000}`},
		{"a float", "float", "2.5", nil, `{"$type":"SimpleIssueCustomField","name":"F","value":2.5}`},
		{"a string stays text", "string", "42", nil, `{"$type":"SimpleIssueCustomField","name":"F","value":"42"}`},
		{"empty clears a single field", "enum[1]", " ", nil, `{"$type":"SingleEnumIssueCustomField","name":"F","value":null}`},
		{"empty clears a multi field", "ownedField[*]", "", nil, `{"$type":"MultiOwnedIssueCustomField","name":"F","value":[]}`},
		{"a period becomes a command", "period", "1w 2d 4h", nil, "command:F 1w2d4h"},
		{"an empty period clears through REST", "period", "", nil, `{"$type":"PeriodIssueCustomField","name":"F","value":null}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := Definition{Name: "F", FieldType: tt.fieldType, CanBeEmpty: true, Values: []string{"Bug", "In Progress"}}
			d.SetUsers(tt.users)
			base, multi := strings.CutSuffix(tt.fieldType, "[*]")
			d.Kind, d.Multi = fieldTypeKinds[strings.TrimSuffix(base, "[1]")], multi
			w, err := d.Encode(tt.raw, opts)
			if err != nil {
				t.Fatal(err)
			}
			got := "command:" + w.Command
			if w.Command == "" {
				b, _ := json.Marshal(w.Field)
				got = string(b)
			}
			if got != tt.want {
				t.Errorf("got  %s\nwant %s", got, tt.want)
			}
		})
	}

	t.Run("given an empty value for a field that cannot be empty, it refuses naming the field", func(t *testing.T) {
		d := Definition{Name: "Priority", FieldType: "enum[1]", Kind: KindEnum, Values: []string{"Major"}}
		if _, err := d.Encode(" ", opts); err == nil || err.Error() != `field "Priority" cannot be empty in this project` {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("given a multi-value field of a kind without a multi-value type, it refuses", func(t *testing.T) {
		for _, ft := range []string{"state[*]", "date[*]"} {
			d := definition(t, `{"canBeEmpty":true,"field":{"name":"F","fieldType":{"id":"`+ft+`"}},"bundle":{"values":[{"name":"Open"}]}}`)
			if _, err := d.Encode("Open", opts); err == nil || !strings.Contains(err.Error(), "use `ytrack issue command`") {
				t.Errorf("%s: err = %v", ft, err)
			}
		}
	})

	t.Run("a group field takes a group from the bundle, and fails closed without the list", func(t *testing.T) {
		d := definition(t, `{"canBeEmpty":true,"field":{"name":"Team","fieldType":{"id":"group[*]"}},"bundle":{"groups":[{"name":"Developers"},{"name":"QA"}]}}`)
		w, err := d.Encode("qa,developers", opts)
		if b, _ := json.Marshal(w.Field); err != nil || string(b) != `{"$type":"MultiGroupIssueCustomField","name":"Team","value":[{"name":"QA"},{"name":"Developers"}]}` {
			t.Errorf("got %s, %v", b, err)
		}
		if _, err := d.Encode("Ops", opts); err == nil || !strings.Contains(err.Error(), "valid values:\n  Developers\n  QA") {
			t.Errorf("err = %v", err)
		}
		blind := definition(t, `{"canBeEmpty":true,"field":{"name":"Team","fieldType":{"id":"group[1]"}}}`)
		if _, err := blind.Encode("QA", opts); err == nil || !strings.Contains(err.Error(), "use `ytrack issue command`") {
			t.Errorf("without groups: err = %v", err)
		}
	})

	t.Run("given many valid values, the error lists the first 20", func(t *testing.T) {
		d := Definition{Name: "Assignee", FieldType: "user[1]", Kind: KindUser, CanBeEmpty: true}
		var users []User
		for i := range 30 {
			users = append(users, User{Login: fmt.Sprintf("user%02d", i)})
		}
		d.SetUsers(users)
		_, err := d.Encode("nobody", opts)
		if err == nil || !strings.Contains(err.Error(), "\n  user18\n  … and 11 more") || strings.Contains(err.Error(), "user19") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("given a value outside the bundle, the error lists the valid values", func(t *testing.T) {
		d := Definition{Name: "Priority", FieldType: "enum[1]", Kind: KindEnum, Values: []string{"Major", "Minor"}}
		_, err := d.Encode("Huge", opts)
		var ve *ValueError
		if !errors.As(err, &ve) || ve.Value != "Huge" || strings.Join(ve.Valid, ",") != "Major,Minor" {
			t.Errorf("err = %v", err)
		}
	})
}

func TestFind(t *testing.T) {
	defs := []Definition{{Name: "Priority"}, {Name: "priority"}, {Name: "Fix versions"}}
	if d, err := Find(defs, "priority", "APP"); err != nil || d.Name != "priority" {
		t.Errorf("an exact match should win: %v %v", d, err)
	}
	if d, err := Find(defs, "FIX VERSIONS", "APP"); err != nil || d.Name != "Fix versions" {
		t.Errorf("case-insensitive match: %v %v", d, err)
	}
	if _, err := Find(defs, "Severity", "APP"); err == nil || !strings.Contains(err.Error(), "project APP has no custom field \"Severity\"") {
		t.Errorf("err = %v", err)
	}
}
