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

	t.Run("given a version or build field whose bundle has no values yet, it refuses naming the field instead of sending the value", func(t *testing.T) {
		for _, ft := range []string{"version[*]", "build[1]"} {
			d := definition(t, `{"canBeEmpty":true,"field":{"name":"Fix versions","fieldType":{"id":"`+ft+`"}},"bundle":{"values":[]}}`)
			_, err := d.Encode("1.0", opts)
			var ve *ValueError
			if !errors.As(err, &ve) || ve.Value != "1.0" || !strings.Contains(err.Error(), "has no values yet") {
				t.Errorf("%s: err = %v", ft, err)
			}
		}
	})

	t.Run("given a bundle YouTrack did not send, it leaves the value to YouTrack", func(t *testing.T) {
		d := definition(t, `{"canBeEmpty":true,"field":{"name":"Fix versions","fieldType":{"id":"version[1]"}}}`)
		if _, err := d.Encode("1.0", opts); err != nil {
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

func TestWriteApplied(t *testing.T) {
	noon := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC).UnixMilli()
	write := func(typ string, value any) Write {
		return Write{Name: "F", Field: map[string]any{"name": "F", "$type": typ, "value": value}}
	}
	read := func(typ, value string) Field {
		return Decode([]byte(`{"name":"F","$type":"` + typ + `","value":` + value + `}`))
	}
	dateTime := func(value string) Field {
		return Decode([]byte(`{"name":"F","$type":"SimpleIssueCustomField","projectCustomField":{"field":{"fieldType":{"id":"date and time"}}},"value":` + value + `}`))
	}
	tests := []struct {
		name string
		w    Write
		f    Field
		want bool
	}{
		{"given an enum read back with the written name, it is applied", write("SingleEnumIssueCustomField", map[string]any{"name": "Task"}), read("SingleEnumIssueCustomField", `{"name":"Task"}`), true},
		{"given an enum read back with the old name, it is not applied", write("SingleEnumIssueCustomField", map[string]any{"name": "Task"}), read("SingleEnumIssueCustomField", `{"name":"Bug"}`), false},
		{"given a multi-enum read back in another order, it is applied", write("MultiEnumIssueCustomField", []any{map[string]any{"name": "A"}, map[string]any{"name": "B"}}), read("MultiEnumIssueCustomField", `[{"name":"B"},{"name":"A"}]`), true},
		{"given a multi-enum read back missing a value, it is not applied", write("MultiEnumIssueCustomField", []any{map[string]any{"name": "A"}, map[string]any{"name": "B"}}), read("MultiEnumIssueCustomField", `[{"name":"A"}]`), false},
		{"given a user read back with another login case, it is applied", write("SingleUserIssueCustomField", map[string]any{"login": "JDoe"}), read("SingleUserIssueCustomField", `{"login":"jdoe","fullName":"John"}`), true},
		{"given a user read back as someone else, it is not applied", write("SingleUserIssueCustomField", map[string]any{"login": "jdoe"}), read("SingleUserIssueCustomField", `{"login":"jroe"}`), false},
		{"given an integer read back equal, it is applied", write("SimpleIssueCustomField", int64(5)), read("SimpleIssueCustomField", `5`), true},
		{"given an integer read back different, it is not applied", write("SimpleIssueCustomField", int64(5)), read("SimpleIssueCustomField", `3`), false},
		{"given a float read back with another spelling, it is applied", write("SimpleIssueCustomField", 1.5), read("SimpleIssueCustomField", `1.50`), true},
		{"given a text read back equal, it is applied", write("TextIssueCustomField", map[string]any{"text": "Null config"}), read("TextIssueCustomField", `{"text":"Null config"}`), true},
		{"given a text read back different, it is not applied", write("TextIssueCustomField", map[string]any{"text": "Null config"}), read("TextIssueCustomField", `{"text":"old"}`), false},
		{"given a string read back equal, it is applied", write("SimpleIssueCustomField", "abc"), read("SimpleIssueCustomField", `"abc"`), true},
		{"given a date read back at UTC noon, it is applied", write("DateIssueCustomField", noon), read("DateIssueCustomField", fmt.Sprint(noon)), true},
		{"given a date stored a day later, it is not applied", write("DateIssueCustomField", noon), read("DateIssueCustomField", fmt.Sprint(noon+24*3600*1000)), false},
		{"given a date stored a day earlier, it is not applied", write("DateIssueCustomField", noon), read("DateIssueCustomField", fmt.Sprint(noon-24*3600*1000)), false},
		{"given a date stored 13h59m later, it is applied", write("DateIssueCustomField", noon), read("DateIssueCustomField", fmt.Sprint(noon+(13*60+59)*60*1000)), true},
		{"given a date stored 13h59m earlier, it is applied", write("DateIssueCustomField", noon), read("DateIssueCustomField", fmt.Sprint(noon-(13*60+59)*60*1000)), true},
		{"given a date stored at a UTC+14 server's local midnight, it is applied", write("DateIssueCustomField", noon), read("DateIssueCustomField", fmt.Sprint(time.Date(2026, 10, 21, 0, 0, 0, 0, time.FixedZone("UTC+14", 14*3600)).UnixMilli())), true},
		{"given a date stored at a UTC-12 server's local midnight, it is applied", write("DateIssueCustomField", noon), read("DateIssueCustomField", fmt.Sprint(time.Date(2026, 10, 20, 0, 0, 0, 0, time.FixedZone("UTC-12", -12*3600)).UnixMilli())), true},
		{"given a date read back two days off, it is not applied", write("DateIssueCustomField", noon), read("DateIssueCustomField", fmt.Sprint(noon+2*24*3600*1000)), false},
		{"given a date-time read back equal, it is applied", write("SimpleIssueCustomField", int64(1791367200000)), dateTime(`1791367200000`), true},
		{"given a date-time read back different, it is not applied", write("SimpleIssueCustomField", int64(1791367200000)), dateTime(`1791367260000`), false},
		{"given a period set by command, it counts as applied", Write{Name: "F", Command: "F 1w"}, read("PeriodIssueCustomField", `null`), true},
		{"given an enum cleared and read back null, it is applied", write("SingleEnumIssueCustomField", nil), read("SingleEnumIssueCustomField", `null`), true},
		{"given an enum cleared but still set, it is not applied", write("SingleEnumIssueCustomField", nil), read("SingleEnumIssueCustomField", `{"name":"Bug"}`), false},
		{"given a multi-enum cleared and read back empty, it is applied", write("MultiEnumIssueCustomField", []any{}), read("MultiEnumIssueCustomField", `[]`), true},
		{"given a user cleared and read back null, it is applied", write("SingleUserIssueCustomField", nil), read("SingleUserIssueCustomField", `null`), true},
		{"given a text cleared and read back as empty text, it is applied", write("TextIssueCustomField", nil), read("TextIssueCustomField", `{"text":""}`), true},
		{"given a text cleared and read back with null text, it is applied", write("TextIssueCustomField", nil), read("TextIssueCustomField", `{"text":null}`), true},
		{"given a text cleared and read back null, it is applied", write("TextIssueCustomField", nil), read("TextIssueCustomField", `null`), true},
		{"given a string cleared and read back as \"\", it is applied", write("SimpleIssueCustomField", nil), read("SimpleIssueCustomField", `""`), true},
		{"given a string cleared and read back null, it is applied", write("SimpleIssueCustomField", nil), read("SimpleIssueCustomField", `null`), true},
		{"given a number cleared but still set, it is not applied", write("SimpleIssueCustomField", nil), read("SimpleIssueCustomField", `5`), false},
		{"given a date cleared and read back null, it is applied", write("DateIssueCustomField", nil), read("DateIssueCustomField", `null`), true},
		{"given a date cleared but still set, it is not applied", write("DateIssueCustomField", nil), read("DateIssueCustomField", fmt.Sprint(noon)), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.w.Applied(tt.f); got != tt.want {
				t.Errorf("Applied = %v, want %v (field %+v)", got, tt.want, tt.f)
			}
		})
	}
}
