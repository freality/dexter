package maputil

import "testing"

func TestSortedKeysOrdersAndCopies(t *testing.T) {
	m := map[string]int{"zeta": 1, "alpha": 2, "mid": 3}
	got := SortedKeys(m)
	want := []string{"alpha", "mid", "zeta"}
	if len(got) != len(want) {
		t.Fatalf("SortedKeys() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("SortedKeys() = %v, want %v", got, want)
		}
	}
}

func TestSortedKeysEmpty(t *testing.T) {
	if got := SortedKeys(map[string]int{}); len(got) != 0 {
		t.Fatalf("SortedKeys() = %v, want empty", got)
	}
}
