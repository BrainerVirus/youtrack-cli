package customfields

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// DefinitionProjection is the fields= projection of a project custom field
// (ProjectCustomField) that Definition decodes.
const DefinitionProjection = "canBeEmpty,field(name,fieldType(id)),bundle(values(name,archived),aggregatedUsers(login,fullName))"

// Definition is a project's custom field: what ytrack needs to write a value
// to it.
type Definition struct {
	Name string
	// FieldType is YouTrack's field type ID, e.g. "enum[1]" or "date and time".
	FieldType  string
	Kind       Kind
	Multi      bool
	CanBeEmpty bool
	// Values are the names of the bundle's elements that are not archived,
	// for enum, state, version, build, owned and group fields.
	Values []string
	// Archived are the archived elements' names; they cannot be set.
	Archived []string
	// Users are the users a user field accepts.
	Users []User
}

// fieldTypeKinds maps YouTrack field type IDs, without the [1] or [*]
// multiplicity suffix, to kinds.
var fieldTypeKinds = map[string]Kind{
	"enum":          KindEnum,
	"state":         KindState,
	"user":          KindUser,
	"group":         KindGroup,
	"version":       KindVersion,
	"build":         KindBuild,
	"ownedField":    KindOwned,
	"period":        KindPeriod,
	"date":          KindDate,
	"date and time": KindDateTime,
	"integer":       KindSimple,
	"float":         KindSimple,
	"string":        KindSimple,
	"text":          KindText,
}

// UnmarshalJSON decodes a wire ProjectCustomField fetched with
// DefinitionProjection.
func (d *Definition) UnmarshalJSON(b []byte) error {
	var w struct {
		CanBeEmpty bool `json:"canBeEmpty"`
		Field      struct {
			Name      string `json:"name"`
			FieldType struct {
				ID string `json:"id"`
			} `json:"fieldType"`
		} `json:"field"`
		Bundle *struct {
			Values []struct {
				Name     string `json:"name"`
				Archived bool   `json:"archived"`
			} `json:"values"`
			AggregatedUsers []User `json:"aggregatedUsers"`
		} `json:"bundle"`
	}
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	ft := w.Field.FieldType.ID
	base, multi := strings.CutSuffix(ft, "[*]")
	base = strings.TrimSuffix(base, "[1]")
	kind, ok := fieldTypeKinds[base]
	if !ok {
		kind = KindUnknown
	}
	*d = Definition{Name: w.Field.Name, FieldType: ft, Kind: kind, Multi: multi, CanBeEmpty: w.CanBeEmpty}
	if w.Bundle != nil {
		for _, v := range w.Bundle.Values {
			if v.Archived {
				d.Archived = append(d.Archived, v.Name)
			} else {
				d.Values = append(d.Values, v.Name)
			}
		}
		d.Users = w.Bundle.AggregatedUsers
	}
	return nil
}

// issueFieldTypes maps a kind to the IssueCustomField $type for single and
// multi-value fields.
var issueFieldTypes = map[Kind][2]string{
	KindEnum:     {"SingleEnumIssueCustomField", "MultiEnumIssueCustomField"},
	KindState:    {"StateIssueCustomField", ""},
	KindUser:     {"SingleUserIssueCustomField", "MultiUserIssueCustomField"},
	KindGroup:    {"SingleGroupIssueCustomField", "MultiGroupIssueCustomField"},
	KindVersion:  {"SingleVersionIssueCustomField", "MultiVersionIssueCustomField"},
	KindBuild:    {"SingleBuildIssueCustomField", "MultiBuildIssueCustomField"},
	KindOwned:    {"SingleOwnedIssueCustomField", "MultiOwnedIssueCustomField"},
	KindPeriod:   {"PeriodIssueCustomField", ""},
	KindDate:     {"DateIssueCustomField", ""},
	KindDateTime: {"SimpleIssueCustomField", ""},
	KindSimple:   {"SimpleIssueCustomField", ""},
	KindText:     {"TextIssueCustomField", ""},
}

// Write is how ytrack sets one custom field: either Field, an
// IssueCustomField for the REST API, or Command, a YouTrack command.
type Write struct {
	Name    string
	Field   map[string]any
	Command string
}

// EncodeOptions carry what Encode needs beyond the definition.
type EncodeOptions struct {
	// Me returns the current user's login, for the value "me".
	Me func() (string, error)
	// Location is the zone of date-time values without an offset.
	Location *time.Location
}

// ValueError is a value a field does not accept.
type ValueError struct {
	Field, Value string
	// Valid are the values the field accepts, when it has a fixed set.
	Valid []string
	Hint  string
}

func (e *ValueError) Error() string {
	msg := fmt.Sprintf("invalid value %q for field %q", e.Value, e.Field)
	if e.Hint != "" {
		msg += ": " + e.Hint
	}
	if len(e.Valid) > 0 {
		msg += "; valid values:\n  " + strings.Join(e.Valid, "\n  ")
	}
	return msg
}

// periodPattern is a YouTrack duration such as "1w 2d 4h 30m".
var periodPattern = regexp.MustCompile(`^(?i)(\d+\s*[wdhm]\s*)+$`)

// Encode turns raw, the text after "Name=", into a Write. An empty raw clears
// the field. Multi-value fields take a comma-separated list, which replaces
// the field's values.
//
// Period fields are set with a YouTrack command ("Estimation 1w2d"), so the
// instance's own work schedule turns days and weeks into minutes; every
// other kind is written through the REST API.
func (d Definition) Encode(raw string, opts EncodeOptions) (Write, error) {
	types, ok := issueFieldTypes[d.Kind]
	if !ok {
		return Write{}, fmt.Errorf("field %q has type %q, which ytrack cannot set with --field; use `ytrack issue command`", d.Name, d.FieldType)
	}
	wireType := types[0]
	if d.Multi && types[1] != "" {
		wireType = types[1]
	}
	w := Write{Name: d.Name, Field: map[string]any{"name": d.Name, "$type": wireType}}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		if d.Multi {
			w.Field["value"] = []any{}
		} else {
			w.Field["value"] = nil
		}
		return w, nil
	}
	parts := []string{raw}
	if d.Multi {
		parts = splitList(raw)
	}
	values := make([]any, 0, len(parts))
	for _, p := range parts {
		v, err := d.encodeOne(p, opts)
		if err != nil {
			return Write{}, err
		}
		if d.Kind == KindPeriod {
			return Write{Name: d.Name, Command: d.Name + " " + v.(string)}, nil
		}
		values = append(values, v)
	}
	if d.Multi {
		w.Field["value"] = values
	} else {
		w.Field["value"] = values[0]
	}
	return w, nil
}

func splitList(s string) []string {
	var out []string
	for p := range strings.SplitSeq(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (d Definition) encodeOne(v string, opts EncodeOptions) (any, error) {
	bad := func(hint string, valid ...string) error {
		return &ValueError{Field: d.Name, Value: v, Hint: hint, Valid: valid}
	}
	switch d.Kind {
	case KindEnum, KindState, KindVersion, KindBuild, KindOwned, KindGroup:
		name, err := d.bundleValue(v)
		if err != nil {
			return nil, err
		}
		return map[string]any{"name": name}, nil
	case KindUser:
		login, err := d.userValue(v, opts)
		if err != nil {
			return nil, err
		}
		return map[string]any{"login": login}, nil
	case KindPeriod:
		if !periodPattern.MatchString(v) {
			return nil, bad("expected a duration such as 1w 2d 4h 30m")
		}
		return strings.Join(strings.Fields(v), ""), nil
	case KindDate:
		t, err := time.Parse(time.DateOnly, v)
		if err != nil {
			return nil, bad("expected a date as YYYY-MM-DD")
		}
		// YouTrack stores dates at midday UTC.
		return t.Add(12 * time.Hour).UnixMilli(), nil
	case KindDateTime:
		t, err := parseDateTime(v, opts.Location)
		if err != nil {
			return nil, bad("expected a date and time as 2026-10-07T14:30 (local) or RFC 3339")
		}
		return t.UnixMilli(), nil
	case KindText:
		return map[string]any{"text": v}, nil
	case KindSimple:
		switch d.FieldType {
		case "integer":
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return nil, bad("expected a whole number")
			}
			return n, nil
		case "float":
			n, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return nil, bad("expected a number")
			}
			return n, nil
		}
		return v, nil
	}
	return nil, errors.New("unreachable")
}

// bundleValue finds v among the field's values: exactly, else ignoring case.
func (d Definition) bundleValue(v string) (string, error) {
	if len(d.Values) == 0 && len(d.Archived) == 0 {
		return v, nil // the bundle was not readable: leave it to YouTrack
	}
	if slices.Contains(d.Values, v) {
		return v, nil
	}
	for _, name := range d.Values {
		if strings.EqualFold(name, v) {
			return name, nil
		}
	}
	hint := ""
	if slices.ContainsFunc(d.Archived, func(a string) bool { return strings.EqualFold(a, v) }) {
		hint = "it is archived"
	}
	return "", &ValueError{Field: d.Name, Value: v, Hint: hint, Valid: d.Values}
}

// userValue resolves "me" and checks v against the field's users by login,
// ignoring case.
func (d Definition) userValue(v string, opts EncodeOptions) (string, error) {
	if v == "me" && opts.Me != nil {
		return opts.Me()
	}
	if len(d.Users) == 0 {
		return v, nil
	}
	logins := make([]string, len(d.Users))
	for i, u := range d.Users {
		if u.Login == v {
			return v, nil
		}
		logins[i] = u.Login
	}
	for _, l := range logins {
		if strings.EqualFold(l, v) {
			return l, nil
		}
	}
	return "", &ValueError{Field: d.Name, Value: v, Hint: "not a login the field accepts", Valid: append([]string{"me"}, logins...)}
}

func parseDateTime(v string, loc *time.Location) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, nil
	}
	if loc == nil {
		loc = time.Local
	}
	for _, layout := range []string{"2006-01-02T15:04", "2006-01-02 15:04"} {
		if t, err := time.ParseInLocation(layout, v, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, errors.New("bad date-time")
}

// UnknownFieldError is a field name the project does not have.
type UnknownFieldError struct {
	Name, Project string
	Valid         []string
}

func (e *UnknownFieldError) Error() string {
	return fmt.Sprintf("project %s has no custom field %q; its fields:\n  %s", e.Project, e.Name, strings.Join(e.Valid, "\n  "))
}

// Find returns the definition called name, exactly or else ignoring case.
func Find(defs []Definition, name, project string) (Definition, error) {
	for _, d := range defs {
		if d.Name == name {
			return d, nil
		}
	}
	names := make([]string, len(defs))
	for i, d := range defs {
		if strings.EqualFold(d.Name, name) {
			return d, nil
		}
		names[i] = d.Name
	}
	return Definition{}, &UnknownFieldError{Name: name, Project: project, Valid: names}
}
