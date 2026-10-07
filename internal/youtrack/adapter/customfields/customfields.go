// Package customfields decodes YouTrack's polymorphic issue custom fields
// into ytrack's own values. It is the one place that knows the wire $type
// names; commands see a Field with a Kind and typed values.
//
// Decoding never fails: a $type this package does not know, or a value that
// does not have the shape its $type promises, degrades to KindUnknown with
// the raw JSON value kept.
package customfields

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// Kind is the domain kind of a custom field, independent of wire $type names.
type Kind string

const (
	KindEnum    Kind = "enum"
	KindState   Kind = "state"
	KindUser    Kind = "user"
	KindGroup   Kind = "group"
	KindVersion Kind = "version"
	KindBuild   Kind = "build"
	KindOwned   Kind = "owned"
	KindPeriod  Kind = "period"
	KindDate    Kind = "date"
	KindText    Kind = "text"
	// KindSimple is a string, integer, float or date-time field: YouTrack
	// sends the bare JSON value and the $type does not say which.
	KindSimple  Kind = "simple"
	KindUnknown Kind = "unknown"
)

// ValueProjection is the fields= projection for custom field values that
// covers every Kind this package decodes.
const ValueProjection = "value(name,login,fullName,minutes,presentation,text)"

// Value is one decoded custom field value.
type Value interface {
	// String is the value as a person reads it.
	String() string
	// Export is the value in ytrack's JSON output.
	Export() any
}

// Named is an element of an enum, state, version, build, owned or group bundle.
type Named struct{ Name string }

func (v Named) String() string { return v.Name }
func (v Named) Export() any    { return v.Name }

// User is a YouTrack account.
type User struct {
	Login    string `json:"login"`
	FullName string `json:"fullName"`
}

func (u User) String() string {
	if u.FullName != "" {
		return u.FullName
	}
	return u.Login
}

func (u User) Export() any { return map[string]any{"login": u.Login, "fullName": u.FullName} }

// Period is a duration as YouTrack stores it: whole minutes, plus the
// instance's own presentation such as "1w 2d 3h".
type Period struct {
	Minutes      int
	Presentation string
}

func (p Period) String() string {
	if p.Presentation != "" {
		return p.Presentation
	}
	return strconv.Itoa(p.Minutes) + "m"
}

func (p Period) Export() any {
	return map[string]any{"minutes": p.Minutes, "presentation": p.Presentation}
}

// Date is a calendar date (YouTrack sends midday UTC in epoch milliseconds).
type Date struct{ Time time.Time }

func (d Date) String() string { return d.Time.UTC().Format(time.DateOnly) }
func (d Date) Export() any    { return d.String() }

// Text is the source text of a text field.
type Text string

func (t Text) String() string { return string(t) }
func (t Text) Export() any    { return string(t) }

// Scalar is the bare value of a simple field: a string or a number.
type Scalar struct{ V any }

func (s Scalar) String() string {
	switch v := s.V.(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	}
	b, _ := json.Marshal(s.V)
	return string(b)
}

func (s Scalar) Export() any { return s.V }

// Raw is a value ytrack could not decode, kept as the JSON YouTrack sent.
type Raw json.RawMessage

func (r Raw) String() string { return string(r) }
func (r Raw) Export() any    { return json.RawMessage(r) }

// Field is one decoded issue custom field.
type Field struct {
	Name string
	Kind Kind
	// Multi is set for multi-value fields; their values are in Values.
	Multi bool
	// Value is a single-value field's value, nil when the field is empty.
	Value Value
	// Values are a multi-value field's values.
	Values []Value
	// WireType is the $type YouTrack sent, for diagnostics.
	WireType string
}

// Empty reports whether the field has no value.
func (f Field) Empty() bool {
	if f.Multi {
		return len(f.Values) == 0
	}
	return f.Value == nil
}

// String renders the value for people; multiple values are comma-separated.
func (f Field) String() string {
	if !f.Multi {
		if f.Value == nil {
			return ""
		}
		return f.Value.String()
	}
	parts := make([]string, len(f.Values))
	for i, v := range f.Values {
		parts[i] = v.String()
	}
	return strings.Join(parts, ", ")
}

// Export renders the value for JSON output: null, a single value, or an array.
func (f Field) Export() any {
	if !f.Multi {
		if f.Value == nil {
			return nil
		}
		return f.Value.Export()
	}
	out := make([]any, len(f.Values))
	for i, v := range f.Values {
		out[i] = v.Export()
	}
	return out
}

// UnmarshalJSON decodes a wire custom field. It never returns an error; see
// Decode.
func (f *Field) UnmarshalJSON(b []byte) error {
	*f = Decode(b)
	return nil
}

type decoder func(json.RawMessage) (Value, bool)

type wireType struct {
	kind   Kind
	multi  bool
	decode decoder
}

// wireTypes maps every IssueCustomField subtype in the YouTrack REST API
// (api/openapi/youtrack.json) to its domain kind and value decoder.
var wireTypes = map[string]wireType{
	"SingleEnumIssueCustomField":    {KindEnum, false, decodeNamed},
	"MultiEnumIssueCustomField":     {KindEnum, true, decodeNamed},
	"StateIssueCustomField":         {KindState, false, decodeNamed},
	"StateMachineIssueCustomField":  {KindState, false, decodeNamed},
	"SingleUserIssueCustomField":    {KindUser, false, decodeUser},
	"MultiUserIssueCustomField":     {KindUser, true, decodeUser},
	"SingleGroupIssueCustomField":   {KindGroup, false, decodeNamed},
	"MultiGroupIssueCustomField":    {KindGroup, true, decodeNamed},
	"SingleVersionIssueCustomField": {KindVersion, false, decodeNamed},
	"MultiVersionIssueCustomField":  {KindVersion, true, decodeNamed},
	"SingleBuildIssueCustomField":   {KindBuild, false, decodeNamed},
	"MultiBuildIssueCustomField":    {KindBuild, true, decodeNamed},
	"SingleOwnedIssueCustomField":   {KindOwned, false, decodeNamed},
	"MultiOwnedIssueCustomField":    {KindOwned, true, decodeNamed},
	"PeriodIssueCustomField":        {KindPeriod, false, decodePeriod},
	"DateIssueCustomField":          {KindDate, false, decodeDate},
	"TextIssueCustomField":          {KindText, false, decodeText},
	"SimpleIssueCustomField":        {KindSimple, false, decodeScalar},
}

// Decode turns one wire IssueCustomField into a Field. Unknown $types and
// values of an unexpected shape become KindUnknown with the raw value.
func Decode(raw []byte) Field {
	var w struct {
		Type  string          `json:"$type"`
		Name  string          `json:"name"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(raw, &w); err != nil {
		return Field{Kind: KindUnknown, Value: rawValue(raw)}
	}
	f := Field{Name: w.Name, WireType: w.Type}
	wt, known := wireTypes[w.Type]
	if !known {
		f.Kind, f.Value = KindUnknown, rawValue(w.Value)
		return f
	}
	f.Kind, f.Multi = wt.kind, wt.multi
	if isNull(w.Value) {
		return f
	}
	if !wt.multi {
		v, ok := wt.decode(w.Value)
		if !ok {
			return unknown(f, w.Value)
		}
		f.Value = v
		return f
	}
	var items []json.RawMessage
	if json.Unmarshal(w.Value, &items) != nil {
		return unknown(f, w.Value)
	}
	f.Values = make([]Value, 0, len(items))
	for _, item := range items {
		v, ok := wt.decode(item)
		if !ok {
			return unknown(f, w.Value)
		}
		f.Values = append(f.Values, v)
	}
	return f
}

func unknown(f Field, value json.RawMessage) Field {
	return Field{Name: f.Name, WireType: f.WireType, Kind: KindUnknown, Value: rawValue(value)}
}

func rawValue(b []byte) Value {
	if isNull(b) {
		return nil
	}
	return Raw(bytes.Clone(b))
}

func isNull(b []byte) bool {
	b = bytes.TrimSpace(b)
	return len(b) == 0 || bytes.Equal(b, []byte("null"))
}

func decodeNamed(b json.RawMessage) (Value, bool) {
	var v struct {
		Name *string `json:"name"`
	}
	if json.Unmarshal(b, &v) != nil || v.Name == nil {
		return nil, false
	}
	return Named{Name: *v.Name}, true
}

func decodeUser(b json.RawMessage) (Value, bool) {
	var v struct {
		Login    *string `json:"login"`
		FullName string  `json:"fullName"`
	}
	if json.Unmarshal(b, &v) != nil || v.Login == nil {
		return nil, false
	}
	return User{Login: *v.Login, FullName: v.FullName}, true
}

func decodePeriod(b json.RawMessage) (Value, bool) {
	var v struct {
		Minutes      *int   `json:"minutes"`
		Presentation string `json:"presentation"`
	}
	if json.Unmarshal(b, &v) != nil || v.Minutes == nil {
		return nil, false
	}
	return Period{Minutes: *v.Minutes, Presentation: v.Presentation}, true
}

func decodeDate(b json.RawMessage) (Value, bool) {
	var ms int64
	if json.Unmarshal(b, &ms) != nil {
		return nil, false
	}
	return Date{Time: time.UnixMilli(ms).UTC()}, true
}

func decodeText(b json.RawMessage) (Value, bool) {
	var v struct {
		Text *string `json:"text"`
	}
	if json.Unmarshal(b, &v) != nil {
		return nil, false
	}
	if v.Text == nil {
		return nil, true
	}
	return Text(*v.Text), true
}

func decodeScalar(b json.RawMessage) (Value, bool) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var v any
	if dec.Decode(&v) != nil {
		return nil, false
	}
	switch v.(type) {
	case string, json.Number:
		return Scalar{V: v}, true
	}
	return nil, false
}
