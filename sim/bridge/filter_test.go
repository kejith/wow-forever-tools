package bridge

import (
	"reflect"
	"testing"
)

func TestParseArgsExcludeFlags(t *testing.T) {
	a, err := ParseArgs([]string{
		"--exclude-item", "12584",
		"--exclude-item", "19514,20041",
		"--exclude-category", "pvp",
	})
	if err != nil {
		t.Fatalf("ParseArgs failed: %v", err)
	}
	if !reflect.DeepEqual(a.ExcludeItems, []int64{12584, 19514, 20041}) {
		t.Errorf("ExcludeItems = %v, want [12584 19514 20041]", a.ExcludeItems)
	}
	if !reflect.DeepEqual(a.ExcludeCategories, []string{"pvp"}) {
		t.Errorf("ExcludeCategories = %v, want [pvp]", a.ExcludeCategories)
	}
	f := a.ItemFilter()
	if f == nil || !f.IsExcludedID(12584) || !f.IsExcludedID(20041) || !f.ExcludesCategory("pvp") {
		t.Errorf("ItemFilter unexpected: %+v", f)
	}

	if _, err := ParseArgs([]string{"--exclude-category", "bogus"}); err == nil {
		t.Error("expected error for unknown category")
	}
	if _, err := ParseArgs([]string{"--exclude-item", "notanumber"}); err == nil {
		t.Error("expected error for invalid item ID")
	}
}
