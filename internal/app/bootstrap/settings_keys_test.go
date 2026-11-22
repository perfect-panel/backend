package bootstrap

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/perfect-panel/backend/internal/config"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
)

// settingsReader describes how one stored settings category is written and
// read. Settings are matched to struct fields by Go field name, so a renamed
// field or key silently stops a setting from applying (the verification-code
// settings were lost that way); these tests turn such a mismatch into a
// failure.
type settingsReader struct {
	category string
	// bootstrap is the struct the bootstrap loader decodes the category into.
	bootstrap any
	// admin is the struct the admin read endpoint decodes the category into.
	admin any
	// written is the admin update request; its field names become the keys
	// the update handler stores (systemsetting.configFields).
	written any
	// unread lists stored keys no reader applies on purpose.
	unread []string
}

var settingsReaders = []settingsReader{
	{category: categorySite, bootstrap: &config.SiteConfig{}, admin: &dto.SiteConfig{}, written: dto.SiteConfig{}},
	{category: categoryInvite, bootstrap: &config.InviteConfig{}, admin: &dto.InviteConfig{}, written: dto.InviteConfig{}},
	{category: categoryRegister, bootstrap: &config.RegisterConfig{}, admin: &dto.RegisterConfig{}, written: dto.RegisterConfig{}},
	{category: categorySubscribe, bootstrap: &config.SubscribeConfig{}, admin: &dto.SubscribeConfig{}, written: dto.SubscribeConfig{}},
	{category: categoryVerify, bootstrap: &verifyConfig{}, admin: &dto.VerifyConfig{}, written: dto.VerifyConfig{}},
	{category: categoryVerifyCode, bootstrap: &verifyCodeSettings{}, admin: &dto.VerifyCodeConfig{}, written: dto.VerifyCodeConfig{}},
	// The node multiplier is read on its own (FindNodeMultiplierConfig), and
	// the admin endpoint decodes node settings into the same database view.
	{category: categoryNode, bootstrap: &config.NodeDBConfig{}, admin: &config.NodeDBConfig{}, written: dto.NodeConfig{}, unread: []string{nodeMultiplierKey}},
	// The seeded Currency key predates CurrencyUnit and has no reader.
	{category: categoryCurrency, bootstrap: &currencySettings{}, admin: &dto.CurrencyConfig{}, written: dto.CurrencyConfig{}, unread: []string{"Currency"}},
}

type storedSetting struct {
	category, key, value, typ, source string
}

func (s storedSetting) ConfigKey() string   { return s.key }
func (s storedSetting) ConfigValue() string { return s.value }
func (s storedSetting) ConfigType() string  { return s.typ }

// Every key the migrations seed or the admin update handler writes must land
// on a field of each reader, with a stored type that field can hold.
func TestStoredSettingKeysMapToReaderFields(t *testing.T) {
	seeded := seededSettings(t)
	for _, reader := range settingsReaders {
		t.Run(reader.category, func(t *testing.T) {
			stored := append(seededIn(seeded, reader.category), writtenSettings(reader, seeded)...)
			if len(stored) == 0 {
				t.Fatal("no stored settings found for the category")
			}
			for _, setting := range stored {
				if slices.Contains(reader.unread, setting.key) {
					continue
				}
				for name, target := range map[string]any{"bootstrap": reader.bootstrap, "admin": reader.admin} {
					field, ok := reflect.TypeOf(target).Elem().FieldByName(setting.key)
					if !ok || !field.IsExported() {
						t.Errorf("%s key %s.%s has no %s field in %T", setting.source, reader.category, setting.key, name, target)
						continue
					}
					fresh := reflect.New(reflect.TypeOf(target).Elem()).Interface()
					if err := config.DecodeSystemConfig([]storedSetting{setting}, fresh); err != nil {
						t.Errorf("%s key %s.%s does not decode into %T: %v", setting.source, reader.category, setting.key, target, err)
					}
				}
			}
		})
	}
}

// The reverse direction: a reader field nothing stores is a field that never
// receives a value, which is how the VerifyCode settings went missing.
func TestReaderFieldsAreBackedByStoredKeys(t *testing.T) {
	seeded := seededSettings(t)
	for _, reader := range settingsReaders {
		keys := map[string]bool{}
		for _, setting := range append(seededIn(seeded, reader.category), writtenSettings(reader, seeded)...) {
			keys[setting.key] = true
		}
		for _, target := range []any{reader.bootstrap, reader.admin} {
			typ := reflect.TypeOf(target).Elem()
			for i := 0; i < typ.NumField(); i++ {
				if field := typ.Field(i); field.IsExported() && !keys[field.Name] {
					t.Errorf("%T.%s has no stored %s key", target, field.Name, reader.category)
				}
			}
		}
	}
}

// writtenSettings returns the settings the admin update handler stores for
// the category: one per request field, holding a non-zero sample value. An
// existing row keeps its seeded type (UpdateValueByCategoryKey only replaces
// the value); a new row takes the type derived from the field.
func writtenSettings(reader settingsReader, seeded []storedSetting) []storedSetting {
	seededTypes := map[string]string{}
	for _, setting := range seededIn(seeded, reader.category) {
		seededTypes[setting.key] = setting.typ
	}
	request := reflect.ValueOf(reader.written)
	var written []storedSetting
	for i := 0; i < request.NumField(); i++ {
		field := request.Type().Field(i)
		sample := sampleValue(field.Type)
		typ, ok := seededTypes[field.Name]
		if !ok {
			typ = writtenType(sample)
		}
		written = append(written, storedSetting{
			category: reader.category, key: field.Name, value: config.ConvertValueToString(sample), typ: typ,
			source: "written",
		})
	}
	return written
}

// writtenType mirrors systemsetting.configFieldType.
func writtenType(value reflect.Value) string {
	if value.Kind() == reflect.Pointer {
		value = reflect.New(value.Type().Elem()).Elem()
	}
	switch value.Kind() {
	case reflect.Bool:
		return "bool"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return "int64"
	case reflect.Interface, reflect.Map, reflect.Slice, reflect.Struct:
		return "interface"
	default:
		return "string"
	}
}

func sampleValue(typ reflect.Type) reflect.Value {
	value := reflect.New(typ).Elem()
	switch typ.Kind() {
	case reflect.Bool:
		value.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value.SetInt(42)
	case reflect.String:
		value.SetString("sample")
	case reflect.Slice:
		value = reflect.Append(value, sampleValue(typ.Elem()))
	case reflect.Pointer:
		pointer := reflect.New(typ.Elem())
		pointer.Elem().Set(sampleValue(typ.Elem()))
		value.Set(pointer)
	}
	return value
}

func seededIn(seeded []storedSetting, category string) []storedSetting {
	var in []storedSetting
	for _, setting := range seeded {
		if setting.category == category {
			in = append(in, setting)
		}
	}
	return in
}

var systemInsert = regexp.MustCompile("(?is)insert\\s+(?:ignore\\s+)?into\\s+[`\"]?system[`\"]?\\s*\\(([^)]*)\\)")

// seededSettings extracts every row the up migrations of both dialects insert
// into the system table.
func seededSettings(t *testing.T) []storedSetting {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "migration", "schema", "database", "*", "*.up.sql"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no migrations found: %v", err)
	}
	var seeded []storedSetting
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		sql := string(data)
		for _, match := range systemInsert.FindAllStringSubmatchIndex(sql, -1) {
			columns := strings.Split(sql[match[2]:match[3]], ",")
			for i, column := range columns {
				columns[i] = strings.ToLower(strings.Trim(strings.TrimSpace(column), "`\""))
			}
			for _, row := range insertedRows(t, file, sql[match[1]:]) {
				if len(row) != len(columns) {
					t.Fatalf("%s: a system row has %d values for %d columns", file, len(row), len(columns))
				}
				values := map[string]*string{}
				for i, column := range columns {
					values[column] = row[i]
				}
				for _, column := range []string{"category", "key", "value", "type"} {
					if values[column] == nil {
						t.Fatalf("%s: a system row has no literal %s", file, column)
					}
				}
				seeded = append(seeded, storedSetting{
					category: *values["category"], key: *values["key"], value: *values["value"], typ: *values["type"],
					source: "seeded (" + filepath.Base(filepath.Dir(file)) + "/" + filepath.Base(file) + ")",
				})
			}
		}
	}
	if len(seeded) < 40 {
		t.Fatalf("found only %d seeded settings; is the migration parser still matching the SQL?", len(seeded))
	}
	return seeded
}

type sqlToken struct {
	text    string
	literal bool
}

// insertedRows parses the VALUES tuples or the SELECT list that follows an
// INSERT column list, up to the end of the statement. A value that is not a
// single string literal is returned as nil.
func insertedRows(t *testing.T, file, rest string) [][]*string {
	t.Helper()
	tokens := tokenizeStatement(rest)
	if len(tokens) == 0 {
		t.Fatalf("%s: INSERT into system without values", file)
	}
	var rows [][]*string
	switch keyword := strings.ToUpper(tokens[0].text); keyword {
	case "VALUES", "VALUE":
		for i := 1; i < len(tokens) && tokens[i].text == "("; {
			row, next := splitValues(tokens, i+1, ")")
			rows = append(rows, row)
			i = next + 1
			if i < len(tokens) && tokens[i].text == "," {
				i++
			}
		}
	case "SELECT":
		row, _ := splitValues(tokens, 1, "WHERE", "FROM")
		rows = append(rows, row)
	default:
		t.Fatalf("%s: unsupported INSERT form starting with %q", file, keyword)
	}
	return rows
}

// splitValues collects comma-separated values starting at tokens[start] up to
// a closing token at nesting depth zero, returning the values and the index
// of the closing token.
func splitValues(tokens []sqlToken, start int, closers ...string) ([]*string, int) {
	var values []*string
	var current []sqlToken
	flush := func() {
		if len(current) == 1 && current[0].literal {
			text := current[0].text
			values = append(values, &text)
		} else {
			values = append(values, nil)
		}
		current = nil
	}
	depth := 0
	for i := start; i < len(tokens); i++ {
		token := tokens[i]
		switch {
		case depth == 0 && !token.literal && slices.Contains(closers, strings.ToUpper(token.text)):
			flush()
			return values, i
		case !token.literal && token.text == "(":
			depth++
		case !token.literal && token.text == ")":
			depth--
		case depth == 0 && !token.literal && token.text == ",":
			flush()
			continue
		}
		current = append(current, token)
	}
	flush()
	return values, len(tokens)
}

// tokenizeStatement splits SQL into string literals, punctuation and words,
// stopping at the first semicolon outside a literal.
func tokenizeStatement(sql string) []sqlToken {
	var tokens []sqlToken
	for i := 0; i < len(sql); {
		c := sql[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '-' && strings.HasPrefix(sql[i:], "--"):
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
		case c == ';':
			return tokens
		case c == '\'':
			var literal strings.Builder
			for i++; i < len(sql); i++ {
				if sql[i] == '\'' {
					if i+1 < len(sql) && sql[i+1] == '\'' {
						literal.WriteByte('\'')
						i++
						continue
					}
					i++
					break
				}
				literal.WriteByte(sql[i])
			}
			tokens = append(tokens, sqlToken{text: literal.String(), literal: true})
		case strings.IndexByte("(),", c) >= 0:
			tokens = append(tokens, sqlToken{text: string(c)})
			i++
		default:
			start := i
			for i < len(sql) && strings.IndexByte(" \t\r\n(),;'", sql[i]) < 0 {
				i++
			}
			tokens = append(tokens, sqlToken{text: sql[start:i]})
		}
	}
	return tokens
}
