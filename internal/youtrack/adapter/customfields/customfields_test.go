package customfields

import (
	"encoding/json"
	"testing"
)

func TestDecode(t *testing.T) {
	tests := []struct {
		name       string
		wire       string
		kind       Kind
		multi      bool
		wantString string
		wantJSON   string
	}{
		{
			"a single enum", `{"name":"Priority","$type":"SingleEnumIssueCustomField","value":{"name":"Major","$type":"EnumBundleElement"}}`,
			KindEnum, false, "Major", `"Major"`,
		},
		{
			"a multi enum", `{"name":"Platform","$type":"MultiEnumIssueCustomField","value":[{"name":"Linux","$type":"EnumBundleElement"},{"name":"macOS","$type":"EnumBundleElement"}]}`,
			KindEnum, true, "Linux, macOS", `["Linux","macOS"]`,
		},
		{
			"a state", `{"name":"State","$type":"StateIssueCustomField","value":{"name":"In Progress","isResolved":false,"$type":"StateBundleElement"}}`,
			KindState, false, "In Progress", `"In Progress"`,
		},
		{
			"a state machine", `{"name":"Stage","$type":"StateMachineIssueCustomField","value":{"name":"Review","$type":"StateBundleElement"}}`,
			KindState, false, "Review", `"Review"`,
		},
		{
			"a single user", `{"name":"Assignee","$type":"SingleUserIssueCustomField","value":{"login":"jdoe","fullName":"John Doe","$type":"User"}}`,
			KindUser, false, "John Doe", `{"fullName":"John Doe","login":"jdoe"}`,
		},
		{
			"a multi user", `{"name":"Reviewers","$type":"MultiUserIssueCustomField","value":[{"login":"ann","fullName":"","$type":"User"},{"login":"bob","fullName":"Bob B","$type":"User"}]}`,
			KindUser, true, "ann, Bob B", `[{"fullName":"","login":"ann"},{"fullName":"Bob B","login":"bob"}]`,
		},
		{
			"a multi version", `{"name":"Fix versions","$type":"MultiVersionIssueCustomField","value":[{"name":"2026.2","$type":"VersionBundleElement"}]}`,
			KindVersion, true, "2026.2", `["2026.2"]`,
		},
		{
			"a period", `{"name":"Estimation","$type":"PeriodIssueCustomField","value":{"minutes":1530,"presentation":"3d 1h 30m","$type":"PeriodValue"}}`,
			KindPeriod, false, "3d 1h 30m", `{"minutes":1530,"presentation":"3d 1h 30m"}`,
		},
		{
			"a date", `{"name":"Due Date","$type":"DateIssueCustomField","value":1791374400000}`,
			KindDate, false, "2026-10-07", `"2026-10-07"`,
		},
		{
			"a text", `{"name":"Notes","$type":"TextIssueCustomField","value":{"text":"line one\nline two","$type":"TextFieldValue"}}`,
			KindText, false, "line one\nline two", `"line one\nline two"`,
		},
		{
			"a simple integer", `{"name":"Story points","$type":"SimpleIssueCustomField","value":8}`,
			KindSimple, false, "8", `8`,
		},
		{
			"a simple string", `{"name":"Customer","$type":"SimpleIssueCustomField","value":"ACME"}`,
			KindSimple, false, "ACME", `"ACME"`,
		},
		{
			"an empty single field", `{"name":"Assignee","$type":"SingleUserIssueCustomField","value":null}`,
			KindUser, false, "", `null`,
		},
		{
			"an empty multi field", `{"name":"Platform","$type":"MultiEnumIssueCustomField","value":[]}`,
			KindEnum, true, "", `[]`,
		},
		{
			"an unknown $type", `{"name":"Mood","$type":"FutureIssueCustomField","value":{"vibe":"good"}}`,
			KindUnknown, false, `{"vibe":"good"}`, `{"vibe":"good"}`,
		},
		{
			"a known $type with a value of the wrong shape", `{"name":"Priority","$type":"SingleEnumIssueCustomField","value":42}`,
			KindUnknown, false, `42`, `42`,
		},
		{
			"a multi field holding a non-array", `{"name":"Platform","$type":"MultiEnumIssueCustomField","value":{"name":"Linux"}}`,
			KindUnknown, false, `{"name":"Linux"}`, `{"name":"Linux"}`,
		},
	}
	for _, tt := range tests {
		t.Run("given "+tt.name+", it decodes to "+string(tt.kind), func(t *testing.T) {
			f := Decode([]byte(tt.wire))
			if f.Kind != tt.kind || f.Multi != tt.multi {
				t.Errorf("kind = %s, multi = %v; want %s, %v", f.Kind, f.Multi, tt.kind, tt.multi)
			}
			if got := f.String(); got != tt.wantString {
				t.Errorf("String() = %q, want %q", got, tt.wantString)
			}
			b, err := json.Marshal(f.Export())
			if err != nil {
				t.Fatal(err)
			}
			if string(b) != tt.wantJSON {
				t.Errorf("Export() = %s, want %s", b, tt.wantJSON)
			}
		})
	}

	t.Run("it keeps the field name and wire $type of an unknown field", func(t *testing.T) {
		f := Decode([]byte(`{"name":"Mood","$type":"FutureIssueCustomField","value":1}`))
		if f.Name != "Mood" || f.WireType != "FutureIssueCustomField" {
			t.Errorf("got %+v", f)
		}
	})

	t.Run("it decodes through json.Unmarshal in a wire struct", func(t *testing.T) {
		var issue struct {
			CustomFields []Field `json:"customFields"`
		}
		err := json.Unmarshal([]byte(`{"customFields":[{"name":"Priority","$type":"SingleEnumIssueCustomField","value":{"name":"Minor"}},"garbage"]}`), &issue)
		if err != nil {
			t.Fatal(err)
		}
		if len(issue.CustomFields) != 2 || issue.CustomFields[0].String() != "Minor" || issue.CustomFields[1].Kind != KindUnknown {
			t.Errorf("got %+v", issue.CustomFields)
		}
	})
}

func FuzzDecode(f *testing.F) {
	for _, seed := range []string{
		`{"name":"P","$type":"SingleEnumIssueCustomField","value":{"name":"x"}}`,
		`{"name":"P","$type":"MultiUserIssueCustomField","value":[{"login":"a"},null,3]}`,
		`{"name":"P","$type":"PeriodIssueCustomField","value":{"minutes":"x"}}`,
		`{"$type":"DateIssueCustomField","value":1e400}`,
		`[]`, `null`, `"`, ``,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		field := Decode(b)
		_ = field.String()
		if _, err := json.Marshal(field.Export()); err != nil && field.Kind != KindUnknown {
			t.Errorf("Export of a decoded %s field does not marshal: %v", field.Kind, err)
		}
	})
}
