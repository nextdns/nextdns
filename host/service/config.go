package service

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type ConfigStorer interface {
	SaveConfig(map[string]ConfigEntry) error
	LoadConfig(map[string]ConfigEntry) error
}

type ConfigEntry interface {
	Set(string) error
	String() string
}

type ConfigDefaultTester interface {
	IsDefault() bool
}

type ConfigListEntry interface {
	ConfigEntry
	Strings() []string
}

type ConfigValue struct {
	Value   *string
	Default string
}

func (e ConfigValue) IsDefault() bool {
	return e.Value == nil || *e.Value == e.Default
}

func (e ConfigValue) Set(v string) error {
	*e.Value = v
	return nil
}

func (e ConfigValue) String() string {
	if e.Value == nil {
		return ""
	}
	return *e.Value
}

type ConfigFlag struct {
	Value   *bool
	Default bool
}

func (e ConfigFlag) IsDefault() bool {
	return e.Value == nil || *e.Value == e.Default
}

func (e ConfigFlag) Set(v string) error {
	switch v {
	case "yes", "true", "1":
		*e.Value = true
	case "no", "false", "0", "":
		*e.Value = false
	default:
		return fmt.Errorf("%v: invalid bool value", v)
	}
	return nil
}

func (e ConfigFlag) String() string {
	if e.Value != nil && *e.Value {
		return "true"
	} else {
		return "false"
	}
}

type ConfigDuration struct {
	Value   *time.Duration
	Default time.Duration
}

func (e ConfigDuration) IsDefault() bool {
	return e.Value == nil || *e.Value == e.Default
}

func (e ConfigDuration) Set(v string) error {
	d, err := time.ParseDuration(v)
	if err != nil {
		return err
	}
	*e.Value = d
	return nil
}

func (e ConfigDuration) String() string {
	if e.Value == nil {
		return ""
	}
	return e.Value.String()
}

type ConfigUint struct {
	Value   *uint
	Default uint
}

func (e ConfigUint) IsDefault() bool {
	return e.Value == nil || *e.Value == e.Default
}

func (e ConfigUint) Set(v string) error {
	d, err := strconv.ParseUint(v, 10, 16)
	if err != nil {
		return err
	}
	*e.Value = uint(d)
	return nil
}

func (e ConfigUint) String() string {
	if e.Value == nil {
		return ""
	}
	return fmt.Sprintf("%d", *e.Value)
}

// parsedValue represents one "key value [# comment]" line from the config file.
type parsedValue struct {
	preComments   []string // consecutive "#" and blank lines immediately above this line
	value         string   // the value portion (trimmed, inline comment stripped)
	inlineComment string   // "" or " # rest of line"
}

// parsedKey collects all values for one key as parsed from the file.
type parsedKey struct {
	values []parsedValue
}

// splitInlineComment splits a raw value string on the first " #" (space then hash).
// Returns (trimmedValue, "") if no inline comment, or (trimmedValue, " # rest").
// A bare "#" with no preceding space is treated as part of the value.
func splitInlineComment(s string) (value, comment string) {
	if idx := strings.Index(s, " #"); idx != -1 {
		return strings.TrimSpace(s[:idx]), s[idx:]
	}
	return s, ""
}

// formatTimestamp returns t formatted as "YYYY.MM.DD : HH:MM:SS".
func formatTimestamp(t time.Time) string {
	return t.Format(time.DateTime)
}

// parseConfigFile reads the config file and returns:
//   - entries: map from key name to its parsed values and associated comments
//   - order: key names in first-appearance order
//   - trailing: comment/blank lines at end of file with no key following them
//
// Returns nil, nil, nil, nil if the file does not exist.
func parseConfigFile(path string) (entries map[string]*parsedKey, order []string, trailing []string, err error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil, nil
		}
		return nil, nil, nil, err
	}
	defer f.Close()

	entries = make(map[string]*parsedKey)
	var pendingComments []string

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			// Blank lines and comment lines accumulate into the pending block.
			// They will be attached as preComments to the next key-value line.
			pendingComments = append(pendingComments, line)
			continue
		}

		// This is a key-value line.
		name := trimmed
		rest := ""
		if idx := strings.IndexByte(trimmed, ' '); idx != -1 {
			name = trimmed[:idx]
			rest = strings.TrimSpace(trimmed[idx+1:])
		}
		value, inlineComment := splitInlineComment(rest)

		pv := parsedValue{
			preComments:   pendingComments,
			value:         value,
			inlineComment: inlineComment,
		}
		pendingComments = nil

		if _, exists := entries[name]; !exists {
			entries[name] = &parsedKey{}
			order = append(order, name)
		}
		entries[name].values = append(entries[name].values, pv)
	}
	if err := sc.Err(); err != nil {
		return nil, nil, nil, err
	}

	trailing = pendingComments
	return entries, order, trailing, nil
}

type ConfigFileStorer struct {
	File string
}

func (s ConfigFileStorer) SaveConfig(c map[string]ConfigEntry) error {
	dir := filepath.Dir(s.File)
	if st, err := os.Stat(dir); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	} else if !st.IsDir() {
		return fmt.Errorf("%s: not a directory", dir)
	}

	oldEntries, fileOrder, trailing, err := parseConfigFile(s.File)
	if err != nil {
		return err
	}

	// Determine output key order:
	// 1. Keys from the old file in their original order (including unknown keys).
	// 2. New keys (in c but not in the old file) sorted alphabetically.
	inFileOrder := make(map[string]bool, len(fileOrder))
	for _, k := range fileOrder {
		inFileOrder[k] = true
	}
	var newKeys []string
	for name := range c {
		if !inFileOrder[name] {
			newKeys = append(newKeys, name)
		}
	}
	sort.Strings(newKeys)
	outputOrder := append(fileOrder, newKeys...)

	f, err := os.OpenFile(s.File, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	now := time.Now()
	for _, name := range outputOrder {
		entry := c[name]
		var old *parsedKey
		if oldEntries != nil {
			old = oldEntries[name]
		}

		if entry == nil {
			// Unknown key from old file — write it back verbatim to avoid data loss.
			if old != nil {
				for _, pv := range old.values {
					writePreComments(f, pv.preComments)
					fmt.Fprintf(f, "%s %s%s\n", name, pv.value, pv.inlineComment)
				}
			}
			continue
		}

		if listEntry, ok := entry.(ConfigListEntry); ok {
			writeListKey(f, name, listEntry.Strings(), old, now)
		} else {
			writeScalarKey(f, name, entry.String(), old)
		}
	}

	// Write any trailing comments/blanks from the end of the old file.
	for _, line := range trailing {
		fmt.Fprintln(f, line)
	}

	return nil
}

func writePreComments(f *os.File, comments []string) {
	for _, c := range comments {
		fmt.Fprintln(f, c)
	}
}

func writeScalarKey(f *os.File, key, newVal string, old *parsedKey) {
	if old != nil && len(old.values) > 0 {
		ov := old.values[0]
		writePreComments(f, ov.preComments)
		fmt.Fprintf(f, "%s %s%s\n", key, newVal, ov.inlineComment)
	} else {
		fmt.Fprintf(f, "%s %s\n", key, newVal)
	}
}

func writeListKey(f *os.File, key string, newVals []string, old *parsedKey, now time.Time) {
	var oldByValue map[string]*parsedValue
	if old != nil {
		oldByValue = make(map[string]*parsedValue, len(old.values))
		for i := range old.values {
			ov := &old.values[i]
			oldByValue[ov.value] = ov
		}
	}
	newValSet := make(map[string]struct{}, len(newVals))
	for _, nv := range newVals {
		newValSet[nv] = struct{}{}
	}

	// Write active values in the order provided by the new config.
	for _, nv := range newVals {
		if ov, exists := oldByValue[nv]; exists {
			writePreComments(f, ov.preComments)
			fmt.Fprintf(f, "%s %s%s\n", key, nv, ov.inlineComment)
		} else {
			fmt.Fprintf(f, "%s %s\n", key, nv)
		}
	}

	// Write removed values (present in old file but absent from new config).
	if old != nil {
		for _, ov := range old.values {
			if _, active := newValSet[ov.value]; !active {
				writePreComments(f, ov.preComments)
				fmt.Fprintf(f, "# Removed %s\n", formatTimestamp(now))
				fmt.Fprintf(f, "# %s %s%s\n", key, ov.value, ov.inlineComment)
			}
		}
	}
}

func (s ConfigFileStorer) LoadConfig(c map[string]ConfigEntry) error {
	f, err := os.Open(s.File)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name := line
		value := ""
		if idx := strings.IndexByte(line, ' '); idx != -1 {
			name = line[:idx]
			value = strings.TrimSpace(line[idx+1:])
		}
		// Strip inline comment before passing to Set so that values like
		// "false # my note" don't cause a parse error.
		value, _ = splitInlineComment(value)
		if entry := c[name]; entry != nil {
			if err := entry.Set(value); err != nil {
				return err
			}
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return nil
}
