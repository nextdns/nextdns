package service

import (
	"os"
	"strings"
	"testing"
)

// testListEntry implements ConfigListEntry for use in tests.
type testListEntry struct {
	vals []string
}

func (e *testListEntry) Set(v string) error {
	for _, existing := range e.vals {
		if existing == v {
			return nil
		}
	}
	e.vals = append(e.vals, v)
	return nil
}

func (e *testListEntry) String() string {
	return strings.Join(e.vals, ",")
}

func (e *testListEntry) Strings() []string {
	return e.vals
}

// writeConfigFile writes content to a temp file and returns its path.
func writeConfigFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp("", "nextdns-config-test-*.conf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })
	return f.Name()
}

// readFile reads the content of a file as a string.
func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func Test_splitInlineComment(t *testing.T) {
	tests := []struct {
		input   string
		value   string
		comment string
	}{
		{"abc123 # default", "abc123", " # default"},
		{"abc123", "abc123", ""},
		{"100MB", "100MB", ""},
		{"10.0.0.1/24=id", "10.0.0.1/24=id", ""},
		{"false # my note", "false", " # my note"},
		{"true", "true", ""},
		{"value # comment with # extra hash", "value", " # comment with # extra hash"},
		{"#notacomment", "#notacomment", ""},  // no space before # → part of value
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, gotC := splitInlineComment(tt.input)
			if got != tt.value || gotC != tt.comment {
				t.Errorf("splitInlineComment(%q) = (%q, %q), want (%q, %q)",
					tt.input, got, gotC, tt.value, tt.comment)
			}
		})
	}
}

func Test_LoadConfig_stripsInlineComment(t *testing.T) {
	path := writeConfigFile(t, "debug false # enable for troubleshooting\n")
	var val bool
	c := map[string]ConfigEntry{
		"debug": ConfigFlag{Value: &val, Default: false},
	}
	storer := ConfigFileStorer{File: path}
	if err := storer.LoadConfig(c); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if val {
		t.Errorf("expected false, got %v", val)
	}
}

func Test_SaveConfig_preservesFullLineComments(t *testing.T) {
	original := "# cache setting\ncache-size 100MB\n"
	path := writeConfigFile(t, original)

	str := "200MB"
	c := map[string]ConfigEntry{
		"cache-size": ConfigValue{Value: &str, Default: "0"},
	}

	if err := (ConfigFileStorer{File: path}).SaveConfig(c); err != nil {
		t.Fatal(err)
	}

	got := readFile(t, path)
	if !strings.Contains(got, "# cache setting\n") {
		t.Errorf("expected full-line comment to be preserved:\n%s", got)
	}
	if !strings.Contains(got, "cache-size 200MB\n") {
		t.Errorf("expected updated value:\n%s", got)
	}
	// Comment must appear immediately before the key
	if !strings.Contains(got, "# cache setting\ncache-size 200MB\n") {
		t.Errorf("comment not directly above key:\n%s", got)
	}
}

func Test_SaveConfig_preservesInlineComment(t *testing.T) {
	original := "cache-size 100MB # large cache\n"
	path := writeConfigFile(t, original)

	str := "200MB"
	c := map[string]ConfigEntry{
		"cache-size": ConfigValue{Value: &str, Default: "0"},
	}

	if err := (ConfigFileStorer{File: path}).SaveConfig(c); err != nil {
		t.Fatal(err)
	}

	got := readFile(t, path)
	if !strings.Contains(got, "cache-size 200MB # large cache\n") {
		t.Errorf("expected inline comment preserved on updated value:\n%s", got)
	}
}

func Test_SaveConfig_preservesKeyOrder(t *testing.T) {
	original := "profile abc123\ncache-size 100MB\n"
	path := writeConfigFile(t, original)

	str := "200MB"
	profile := &testListEntry{vals: []string{"abc123"}}
	c := map[string]ConfigEntry{
		"cache-size": ConfigValue{Value: &str, Default: "0"},
		"profile":    profile,
	}

	if err := (ConfigFileStorer{File: path}).SaveConfig(c); err != nil {
		t.Fatal(err)
	}

	got := readFile(t, path)
	profileIdx := strings.Index(got, "profile")
	cacheIdx := strings.Index(got, "cache-size")
	if profileIdx == -1 || cacheIdx == -1 {
		t.Fatalf("missing keys in output:\n%s", got)
	}
	// profile appeared first in original file, must stay first
	if profileIdx > cacheIdx {
		t.Errorf("expected profile before cache-size (original order):\n%s", got)
	}
}

func Test_SaveConfig_newKeysAppendedAlphabetically(t *testing.T) {
	// File only has "profile"; "cache-size" and "debug" are new
	original := "profile abc123\n"
	path := writeConfigFile(t, original)

	str := "100MB"
	dbg := false
	profile := &testListEntry{vals: []string{"abc123"}}
	c := map[string]ConfigEntry{
		"cache-size": ConfigValue{Value: &str, Default: "0"},
		"debug":      ConfigFlag{Value: &dbg, Default: false},
		"profile":    profile,
	}

	if err := (ConfigFileStorer{File: path}).SaveConfig(c); err != nil {
		t.Fatal(err)
	}

	got := readFile(t, path)
	profileIdx := strings.Index(got, "profile")
	cacheIdx := strings.Index(got, "cache-size")
	debugIdx := strings.Index(got, "debug")

	// profile is from old file → comes first
	if profileIdx > cacheIdx || profileIdx > debugIdx {
		t.Errorf("old key 'profile' should appear before new keys:\n%s", got)
	}
	// cache-size (c) comes before debug (d) alphabetically
	if cacheIdx > debugIdx {
		t.Errorf("new keys should be alphabetically ordered (cache-size before debug):\n%s", got)
	}
}

func Test_SaveConfig_removedListValueCommentedOut(t *testing.T) {
	original := "# work rule\nprofile 10.0.0.0/8=abc123 # work\nprofile def456\n"
	path := writeConfigFile(t, original)

	// Keep only def456; the work rule is removed
	profile := &testListEntry{vals: []string{"def456"}}
	c := map[string]ConfigEntry{
		"profile": profile,
	}

	if err := (ConfigFileStorer{File: path}).SaveConfig(c); err != nil {
		t.Fatal(err)
	}

	got := readFile(t, path)

	// Active value must still be written (may be at start of file, so no leading \n required)
	if !strings.Contains(got, "profile def456\n") {
		t.Errorf("expected active value 'def456':\n%s", got)
	}
	// Removed value must be commented out
	if !strings.Contains(got, "# Removed ") {
		t.Errorf("expected removal timestamp comment:\n%s", got)
	}
	if !strings.Contains(got, "# profile 10.0.0.0/8=abc123 # work\n") {
		t.Errorf("expected removed line with inline comment:\n%s", got)
	}
	// Pre-comment for removed value must be preserved
	if !strings.Contains(got, "# work rule\n") {
		t.Errorf("expected pre-comment for removed value:\n%s", got)
	}
	// Removed value must NOT appear as an active line
	if strings.Contains(got, "\nprofile 10.0.0.0/8=abc123") {
		t.Errorf("removed value should not appear as active:\n%s", got)
	}
}

func Test_SaveConfig_trailingCommentsPreserved(t *testing.T) {
	original := "cache-size 100MB\n# trailing note\n"
	path := writeConfigFile(t, original)

	str := "100MB"
	c := map[string]ConfigEntry{
		"cache-size": ConfigValue{Value: &str, Default: "0"},
	}

	if err := (ConfigFileStorer{File: path}).SaveConfig(c); err != nil {
		t.Fatal(err)
	}

	got := readFile(t, path)
	if !strings.Contains(got, "# trailing note") {
		t.Errorf("expected trailing comment preserved:\n%s", got)
	}
}

func Test_SaveConfig_blankLineBetweenCommentAndKey(t *testing.T) {
	// Blank line between comment and key should NOT break association.
	original := "# my cache\n\ncache-size 100MB\n"
	path := writeConfigFile(t, original)

	str := "200MB"
	c := map[string]ConfigEntry{
		"cache-size": ConfigValue{Value: &str, Default: "0"},
	}

	if err := (ConfigFileStorer{File: path}).SaveConfig(c); err != nil {
		t.Fatal(err)
	}

	got := readFile(t, path)
	// The blank line and comment should travel with cache-size
	if !strings.Contains(got, "# my cache\n\ncache-size 200MB\n") {
		t.Errorf("expected blank-line+comment preserved above key:\n%s", got)
	}
}

func Test_SaveConfig_unknownOldKeyPreserved(t *testing.T) {
	// Keys not registered in c should be written back unchanged.
	original := "unknown-future-key somevalue\ncache-size 100MB\n"
	path := writeConfigFile(t, original)

	str := "100MB"
	c := map[string]ConfigEntry{
		"cache-size": ConfigValue{Value: &str, Default: "0"},
	}

	if err := (ConfigFileStorer{File: path}).SaveConfig(c); err != nil {
		t.Fatal(err)
	}

	got := readFile(t, path)
	if !strings.Contains(got, "unknown-future-key somevalue\n") {
		t.Errorf("expected unknown key to be preserved:\n%s", got)
	}
}

func Test_SaveConfig_noOldFile(t *testing.T) {
	path := t.TempDir() + "/nextdns.conf"

	str := "100MB"
	c := map[string]ConfigEntry{
		"cache-size": ConfigValue{Value: &str, Default: "0"},
	}

	if err := (ConfigFileStorer{File: path}).SaveConfig(c); err != nil {
		t.Fatal(err)
	}

	got := readFile(t, path)
	if !strings.Contains(got, "cache-size 100MB\n") {
		t.Errorf("expected key written to new file:\n%s", got)
	}
}
