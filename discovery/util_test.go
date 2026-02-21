package discovery

import (
	"reflect"
	"testing"
)

func TestAppendUniq_InsertSortedAndDedup(t *testing.T) {
	set := []string{"b", "d"}
	got := appendUniq(set, "c", "a", "d", "e")
	want := []string{"a", "b", "c", "d", "e"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("appendUniq() = %v, want %v", got, want)
	}
}
