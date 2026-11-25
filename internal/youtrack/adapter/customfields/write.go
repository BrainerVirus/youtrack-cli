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
// (ProjectCustomField) that Definition decodes. It leaves out the users of
// user fields, which can be many; UsersProjection fetches them for one field.
const DefinitionProjection = "id,canBeEmpty,field(name,fieldType(id)),bundle(values(name,archived),groups(name))"

// SettingsProjection is DefinitionProjection with the field's default
// values (bundle elements and groups by name, users by login), which
// Definition decodes into Defaults.
const SettingsProjection = DefinitionProjection + ",defaultValues(name,login)"

// UsersProjection is the fields= projection of one user field's users.
const UsersProjection = "bundle(aggregatedUsers(login,fullName))"

// maxListed caps the valid values an error lists.
const maxListed = 20

// Definition is a project's custom field: what ytrack needs to write a value
// to it.
type Definition struct {
	// ID is the project custom field's database ID.
	ID   string
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
	// Groups are the user groups a group field accepts; nil when YouTrack
	// did not send them.
	Groups []string
	// Defaults are the names (logins for users) of the values a new issue
	// gets, when fetched with SettingsProjection.
	Defaults []string
	// Users are the users a user field accepts, once SetUsers was called.
	Users       []User
	usersLoaded bool
	// valuesRead is whether YouTrack sent the bundle's values, even none:
	// an empty version bundle then refuses a value rather than passing it on.
	valuesRead bool
}

// SetUsers records the users a user field accepts (see UsersProjection).
func (d *Definition) SetUsers(users []User) { d.Users, d.usersLoaded = users, true }

// NeedsUsers reports whether a user field's users are not loaded yet.
func (d Definition) NeedsUsers() bool { return d.Kind == KindUser && !d.usersLoaded }

// DecodeUsers decodes the users of a project custom field fetched with
// UsersProjection.
func DecodeUsers(b []byte) ([]User, error) {
	var w struct {
		Bundle *struct {
			AggregatedUsers []User `json:"aggregatedUsers"`
		} `json:"bundle"`
	}
	if err := json.Unmarshal(b, &w); err != nil || w.Bundle == nil {
		return nil, err
	}
	return w.Bundle.AggregatedUsers, nil
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
// DefinitionProjection or SettingsProjection.
func (d *Definition) UnmarshalJSON(b []byte) error {
	var w struct {
		ID         string `json:"id"`
		CanBeEmpty bool   `json:"canBeEmpty"`
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
			Groups []struct {
				Name string `json:"name"`
			} `json:"groups"`
		} `json:"bundle"`
		// DefaultValues is an object for a single-value default on some
		// YouTrack versions and an array otherwise.
		DefaultValues json.RawMessage `json:"defaultValues"`
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
	*d = Definition{ID: w.ID, Name: w.Field.Name, FieldType: ft, Kind: kind, Multi: multi, CanBeEmpty: w.CanBeEmpty}
	d.Defaults = decodeDefaults(w.DefaultValues)
	if w.Bundle != nil {
		d.valuesRead = w.Bundle.Values != nil
		for _, v := range w.Bundle.Values {
			if v.Archived {
				d.Archived = append(d.Archived, v.Name)
			} else {
				d.Values = append(d.Values, v.Name)
			}
		}
		if w.Bundle.Groups != nil {
			d.Groups = []string{}
			for _, g := range w.Bundle.Groups {
				d.Groups = append(d.Groups, g.Name)
			}
		}
	}
	return nil
}

// decodeDefaults reads defaultValues, an array or a single value, each
// element named by its name or else its login.
func decodeDefaults(raw json.RawMessage) []string {
	type named struct {
		Name  string `json:"name"`
		Login string `json:"login"`
	}
	var many []named
	if err := json.Unmarshal(raw, &many); err != nil {
		var one *named
		if json.Unmarshal(raw, &one) != nil || one == nil {
			return nil
		}
		many = []named{*one}
	}
	var out []string
	for _, v := range many {
		if v.Name != "" {
			out = append(out, v.Name)
		} else if v.Login != "" {
			out = append(out, v.Login)
		}
	}
	return out
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

// WriteTypes are the IssueCustomField $types Encode can produce.
func WriteTypes() []string {
	var out []string
	for _, pair := range issueFieldTypes {
		for _, t := range pair {
			if t != "" && !slices.Contains(out, t) {
				out = append(out, t)
			}
		}
	}
	slices.Sort(out)
	return out
}

// Write is how ytrack sets one custom field: either Field, an
// IssueCustomField for the REST API, or Command, a YouTrack command.
type Write struct {
	Name    string
	Field   map[string]any
	Command string
	// Period is the duration a period Command sets ("1w2d"), which Applied
	// compares with the field read back.
	Period string
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
		msg += "; valid values:\n  " + strings.Join(capList(e.Valid), "\n  ")
	}
	return msg
}

// capList keeps the first maxListed items and says how many were left out.
func capList(items []string) []string {
	if len(items) <= maxListed {
		return items
	}
	return append(slices.Clone(items[:maxListed]), fmt.Sprintf("… and %d more", len(items)-maxListed))
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
	if d.Multi {
		if types[1] == "" {
			return Write{}, fmt.Errorf("field %q is a multi-value %s field, which ytrack cannot set with --field; use `ytrack issue command`", d.Name, d.FieldType)
		}
		wireType = types[1]
	}
	if d.Kind == KindGroup && d.Groups == nil {
		return Write{}, fmt.Errorf("YouTrack did not list the groups field %q accepts, so ytrack does not set it; use `ytrack issue command`", d.Name)
	}
	w := Write{Name: d.Name, Field: map[string]any{"name": d.Name, "$type": wireType}}
	raw = strings.TrimSpace(raw)
	if raw == "" && !d.CanBeEmpty {
		return Write{}, fmt.Errorf("field %q cannot be empty in this project", d.Name)
	}
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
			return Write{Name: d.Name, Command: d.Name + " " + v.(string), Period: v.(string)}, nil
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
	case KindGroup:
		for _, g := range d.Groups {
			if strings.EqualFold(g, v) {
				return map[string]any{"name": g}, nil
			}
		}
		return nil, bad("not a group the field accepts", d.Groups...)
	case KindEnum, KindState, KindVersion, KindBuild, KindOwned:
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
		if !d.valuesRead {
			return v, nil // the bundle was not readable: leave it to YouTrack
		}
		return "", &ValueError{Field: d.Name, Value: v, Hint: "the field has no values yet; add " + v + " to its bundle in the project settings first"}
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
	if !d.usersLoaded || len(d.Users) == 0 {
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
	return fmt.Sprintf("project %s has no custom field %q (`ytrack project fields %s` shows their types and values); its fields:\n  %s",
		e.Project, e.Name, e.Project, strings.Join(e.Valid, "\n  "))
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

// Applied reports whether f, the field read back after w was written,
// holds the value w wrote. A value it cannot compare (a field it cannot
// decode) counts as applied. An empty string or text reads back as cleared,
// and a date matches when the stored instant is within 14 hours of the
// midday-UTC instant written (a server normalizing dates to midnight in its
// own zone). A period set by command is compared by periodApplied.
func (w Write) Applied(f Field) bool {
	return w.AppliedUnder(f, WorkSchedule{})
}

// WorkSchedule is the instance's work schedule, which turns days and weeks
// into minutes. The zero value means it is unknown.
type WorkSchedule struct {
	MinutesADay int
	DaysAWeek   int
}

// Known reports whether s can convert days and weeks into minutes.
func (s WorkSchedule) Known() bool { return s.MinutesADay > 0 && s.DaysAWeek > 0 }

// AppliedUnder is Applied with the instance's work schedule: under a known
// one, a period set by command is applied exactly when the field read back
// holds the minutes the duration comes to.
func (w Write) AppliedUnder(f Field, s WorkSchedule) bool {
	if w.Period != "" && f.Kind != KindUnknown {
		if s.Known() {
			return periodAppliedExactly(w.Period, f.Value, s)
		}
		return periodApplied(w.Period, f.Value)
	}
	if w.Field == nil || f.Kind == KindUnknown {
		return true
	}
	var want []string
	switch v := w.Field["value"].(type) {
	case nil:
	case []any:
		for _, item := range v {
			want = append(want, writtenKey(item, f.Kind))
		}
	default:
		want = []string{writtenKey(v, f.Kind)}
	}
	want = slices.DeleteFunc(want, func(k string) bool { return k == "" })
	values := f.Values
	if !f.Multi {
		values = nil
		if f.Value != nil {
			values = []Value{f.Value}
		}
	}
	if ms, ok := w.Field["value"].(int64); ok && f.Kind == KindDate && len(values) == 1 {
		if d, ok := values[0].(Date); ok {
			return d.Time.Sub(time.UnixMilli(ms)).Abs() <= dateTolerance
		}
	}
	var got []string
	for _, v := range values {
		if k := valueKey(v); k != "" {
			got = append(got, k)
		}
	}
	slices.Sort(want)
	slices.Sort(got)
	return slices.Equal(want, got)
}

// dateTolerance is the widest UTC offset (UTC+14) a server can move a
// date's stored instant by when it normalizes it to its own midnight.
const dateTolerance = 14 * time.Hour

// writtenKey is the comparable form of one value Encode wrote.
func writtenKey(v any, kind Kind) string {
	switch v := v.(type) {
	case map[string]any:
		for _, k := range []string{"name", "login"} {
			if s, ok := v[k].(string); ok {
				return strings.ToLower(s)
			}
		}
		s, _ := v["text"].(string)
		return s
	case int64:
		switch kind {
		case KindDate:
			return Date{Time: time.UnixMilli(v)}.String()
		case KindDateTime:
			return strconv.FormatInt(v, 10)
		}
		return numberKey(float64(v))
	case float64:
		return numberKey(v)
	}
	return fmt.Sprint(v)
}

// valueKey is the comparable form of a value read back.
func valueKey(v Value) string {
	switch v := v.(type) {
	case Named:
		return strings.ToLower(v.Name)
	case User:
		return strings.ToLower(v.Login)
	case DateTime:
		return strconv.FormatInt(v.Time.UnixMilli(), 10)
	case Scalar:
		if n, ok := v.V.(json.Number); ok {
			if f, err := n.Float64(); err == nil {
				return numberKey(f)
			}
		}
	}
	return v.String()
}

func numberKey(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// periodAppliedExactly reports whether got, a period field read back after
// the command that set it to want, holds the minutes want comes to under s.
func periodAppliedExactly(want string, got Value, s WorkSchedule) bool {
	p, ok := got.(Period)
	if !ok {
		return false
	}
	w, wok := periodUnits(want)
	if !wok {
		return true
	}
	minutes := (w['w']*s.DaysAWeek+w['d'])*s.MinutesADay + w['h']*60 + w['m']
	return minutes == p.Minutes
}

// periodApplied reports whether got, a period field read back after the
// command that set it to want ("1w2d"), holds that duration, when the
// instance's work schedule is unknown. That schedule turns days and weeks into
// minutes and can rewrite the presentation ("10d" as "2w"), so it is false
// only when that is certain. The same units always match. Otherwise a want
// in hours and minutes alone must have the same minutes. The presentation,
// which YouTrack derives from the minutes, tells the rest: one in days and
// no weeks gives the minutes in a day, so a want without weeks must come to
// the same minutes; one under a day cannot hold a want of a day or more;
// and a read-back of zero minutes never holds a want of days or weeks.
func periodApplied(want string, got Value) bool {
	p, ok := got.(Period)
	if !ok {
		return false
	}
	w, wok := periodUnits(want)
	if !wok {
		return true
	}
	g, gok := periodUnits(p.Presentation)
	if gok && w == g {
		return true
	}
	wantClock := w['h']*60 + w['m']
	if w['w'] == 0 && w['d'] == 0 {
		return wantClock == p.Minutes
	}
	if p.Minutes <= 0 {
		return false
	}
	if !gok || g['w'] > 0 {
		return true
	}
	if g['d'] == 0 {
		return false
	}
	rest := p.Minutes - g['h']*60 - g['m']
	if rest <= 0 || rest%g['d'] != 0 || w['w'] > 0 {
		return true
	}
	return w['d']*(rest/g['d'])+wantClock == p.Minutes
}

// periodUnits sums a duration such as "1w 2d 4h" or "90m" by unit, with
// minutes over 59 carried into hours; ok is false when s is not a duration.
func periodUnits(s string) (units [128]int, ok bool) {
	s = strings.ToLower(strings.Join(strings.Fields(s), ""))
	if !periodPattern.MatchString(s) {
		return units, false
	}
	n := 0
	for i := 0; i < len(s); i++ {
		if c := s[i]; c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
			continue
		}
		units[s[i]] += n
		n = 0
	}
	units['h'] += units['m'] / 60
	units['m'] %= 60
	return units, true
}
