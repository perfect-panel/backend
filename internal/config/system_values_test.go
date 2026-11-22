package config

import (
	"errors"
	"strings"
	"testing"

	"github.com/perfect-panel/backend/pkg/logger/logtest"
)

type testSystemConfigEntry struct {
	key   string
	value string
	typ   string
}

func (e testSystemConfigEntry) ConfigKey() string   { return e.key }
func (e testSystemConfigEntry) ConfigValue() string { return e.value }
func (e testSystemConfigEntry) ConfigType() string  { return e.typ }

func TestSystemConfigSliceReflectToStructUsesNarrowContract(t *testing.T) {
	target := struct {
		Name    string
		Enabled *bool
		Retries int
		Limit   int64
		Labels  []string
	}{}

	SystemConfigSliceReflectToStruct([]testSystemConfigEntry{
		{key: "Name", value: "PPanel", typ: "string"},
		{key: "Enabled", value: "true", typ: "bool"},
		{key: "Retries", value: "3", typ: "int"},
		{key: "Limit", value: "42", typ: "int64"},
		{key: "Labels", value: `["admin","user"]`, typ: "interface"},
	}, &target)

	if target.Name != "PPanel" || target.Enabled == nil || !*target.Enabled || target.Retries != 3 || target.Limit != 42 {
		t.Fatalf("unexpected scalar values: %#v", target)
	}
	if len(target.Labels) != 2 || target.Labels[0] != "admin" || target.Labels[1] != "user" {
		t.Fatalf("unexpected labels: %#v", target.Labels)
	}
}

func TestSystemConfigJSONCompatibility(t *testing.T) {
	target := struct {
		Options struct {
			Max     int64 `json:"max"`
			Enabled bool  `json:"enabled"`
		}
		Labels []string
		Extra  map[string]any
	}{Labels: []string{"old"}}
	SystemConfigSliceReflectToStruct([]testSystemConfigEntry{
		{key: "Options", value: `{"max":9007199254740993,"enabled":false}`, typ: "interface"},
		{key: "Labels", value: `null`, typ: "interface"},
		{key: "Extra", value: `{"count":2,"enabled":false}`, typ: "interface"},
	}, &target)
	if target.Options.Max != 9007199254740993 || target.Options.Enabled || target.Labels != nil || target.Extra["count"] != float64(2) || target.Extra["enabled"] != false {
		t.Fatalf("JSON conversion changed: %+v", target)
	}
}

// Values that do not parse used to vanish silently; they are now reported per
// key while every other setting still applies, and the failing field keeps
// the zero value the silent implementation produced.
func TestDecodeSystemConfigReportsUnparseableValuesByKey(t *testing.T) {
	target := struct {
		Name     string
		Retries  int64
		Enabled  bool
		Optional *bool
		Labels   []string
	}{Retries: 9, Enabled: true}

	err := DecodeSystemConfig([]testSystemConfigEntry{
		{key: "Name", value: "PPanel", typ: "string"},
		{key: "Retries", value: "three", typ: "int"},
		{key: "Enabled", value: "maybe", typ: "bool"},
		{key: "Optional", value: "sometimes", typ: "bool"},
		{key: "Labels", value: `["unterminated"`, typ: "interface"},
	}, &target)

	if target.Name != "PPanel" {
		t.Fatalf("a failing setting blocked the valid ones: %+v", target)
	}
	if target.Retries != 0 || target.Enabled || target.Optional == nil || *target.Optional {
		t.Fatalf("unparseable values must leave the zero value: %+v", target)
	}
	var settingErr *SettingError
	if !errors.As(err, &settingErr) {
		t.Fatalf("error %v carries no *SettingError", err)
	}
	for _, key := range []string{"Retries", "Enabled", "Optional", "Labels"} {
		if !strings.Contains(err.Error(), "setting "+key+" ") {
			t.Fatalf("error %v does not report %s", err, key)
		}
	}
	if strings.Contains(err.Error(), "setting Name ") {
		t.Fatalf("valid setting reported as failing: %v", err)
	}
}

// Empty values are how the seed data stores "unset" (TrialSubscribe is an
// empty int); they must not be reported on every start.
func TestDecodeSystemConfigTreatsEmptyValuesAsUnset(t *testing.T) {
	optional := true
	target := struct {
		TrialSubscribe int64
		Enabled        bool
		Optional       *bool
		Labels         []string
	}{TrialSubscribe: 5, Enabled: true, Optional: &optional, Labels: []string{"kept"}}

	err := DecodeSystemConfig([]testSystemConfigEntry{
		{key: "TrialSubscribe", value: "", typ: "int"},
		{key: "Enabled", value: "", typ: "bool"},
		{key: "Optional", value: "", typ: "bool"},
		{key: "Labels", value: "", typ: "interface"},
	}, &target)

	if err != nil {
		t.Fatalf("empty values reported as errors: %v", err)
	}
	if target.TrialSubscribe != 0 || target.Enabled || target.Optional != nil || len(target.Labels) != 1 {
		t.Fatalf("empty values decoded differently than before: %+v", target)
	}
}

// A stored type the field cannot hold used to panic inside reflect; it is now
// reported and the field left alone. The report must not echo the value,
// which can be a secret.
func TestDecodeSystemConfigReportsStoredTypeMismatchWithoutPanicking(t *testing.T) {
	target := struct {
		Secret string
		Limit  int64
		Other  string
	}{Secret: "unchanged", Limit: 7}

	err := DecodeSystemConfig([]testSystemConfigEntry{
		{key: "Secret", value: "s3cr3t-value", typ: "bool"},
		{key: "Limit", value: "12", typ: "string"},
		{key: "Other", value: "x", typ: "float"},
		{key: "Unknown", value: "ignored", typ: "string"},
	}, &target)

	if target.Secret != "unchanged" || target.Limit != 7 || target.Other != "" {
		t.Fatalf("mismatched settings changed their fields: %+v", target)
	}
	for _, want := range []string{"setting Secret ", "setting Limit ", "setting Other ", "unsupported stored type"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("error %v, want it to contain %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "s3cr3t-value") {
		t.Fatalf("error echoes the stored value: %v", err)
	}
	if strings.Contains(err.Error(), "Unknown") {
		t.Fatalf("a key without a field is not an error: %v", err)
	}
}

func TestDecodeSystemConfigRejectsNonStructTargets(t *testing.T) {
	var notStruct int
	for _, target := range []any{nil, struct{}{}, &notStruct, (*struct{})(nil)} {
		if err := DecodeSystemConfig([]testSystemConfigEntry{{key: "A", value: "b", typ: "string"}}, target); err == nil {
			t.Fatalf("DecodeSystemConfig(%T) returned no error", target)
		}
	}
}

// Callers that cannot act on the error (the admin read endpoints) still get
// the failure logged with its key.
func TestSystemConfigSliceReflectToStructLogsFailuresWithKey(t *testing.T) {
	logs := logtest.NewCollector(t)
	target := struct{ ClearDays int64 }{}

	SystemConfigSliceReflectToStruct([]testSystemConfigEntry{{key: "ClearDays", value: "seven", typ: "int64"}}, &target)

	if out := logs.String(); !strings.Contains(out, "ClearDays") || !strings.Contains(out, "could not be applied") {
		t.Fatalf("log = %s, want the failing key reported", out)
	}
}

// The admin settings update stores list and object fields as "interface"
// rows. A reader that keeps such a setting as JSON text (NodeDBConfig's DNS,
// Block and Outbound) must get the text back, not a decode error that loses
// the setting; a structured field still decodes the document.
func TestDecodeSystemConfigKeepsInterfaceRowsAsTextForStringFields(t *testing.T) {
	var target struct {
		DNS    string
		Labels []string
	}
	err := DecodeSystemConfig([]testSystemConfigEntry{
		{key: "DNS", value: `[{"proto":"udp","address":"1.1.1.1"}]`, typ: "interface"},
		{key: "Labels", value: `["a","b"]`, typ: "interface"},
	}, &target)
	if err != nil {
		t.Fatalf("interface rows reported as errors: %v", err)
	}
	if target.DNS != `[{"proto":"udp","address":"1.1.1.1"}]` {
		t.Fatalf("DNS = %q, want the stored JSON text", target.DNS)
	}
	if len(target.Labels) != 2 || target.Labels[1] != "b" {
		t.Fatalf("Labels = %v, want the decoded list", target.Labels)
	}
}
