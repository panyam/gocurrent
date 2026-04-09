package gocurrent

import (
	"sync"
	"testing"
)

func TestSyncMap_StoreAndLoad(t *testing.T) {
	var m SyncMap[string, int]
	m.Store("a", 1)
	m.Store("b", 2)

	v, ok := m.Load("a")
	if !ok || v != 1 {
		t.Errorf("Load(a) = (%d, %v), want (1, true)", v, ok)
	}
	v, ok = m.Load("b")
	if !ok || v != 2 {
		t.Errorf("Load(b) = (%d, %v), want (2, true)", v, ok)
	}
}

func TestSyncMap_LoadMissing(t *testing.T) {
	var m SyncMap[string, int]
	v, ok := m.Load("missing")
	if ok {
		t.Errorf("Load(missing) = (%d, true), want (0, false)", v)
	}
	if v != 0 {
		t.Errorf("Load(missing) zero value = %d, want 0", v)
	}
}

func TestSyncMap_Delete(t *testing.T) {
	var m SyncMap[string, int]
	m.Store("a", 1)
	m.Delete("a")

	_, ok := m.Load("a")
	if ok {
		t.Error("Load(a) after Delete should return false")
	}
}

func TestSyncMap_LoadAndDelete(t *testing.T) {
	var m SyncMap[string, int]
	m.Store("a", 42)

	v, loaded := m.LoadAndDelete("a")
	if !loaded || v != 42 {
		t.Errorf("LoadAndDelete(a) = (%d, %v), want (42, true)", v, loaded)
	}

	_, ok := m.Load("a")
	if ok {
		t.Error("Load(a) after LoadAndDelete should return false")
	}
}

func TestSyncMap_LoadAndDelete_Missing(t *testing.T) {
	var m SyncMap[string, int]
	v, loaded := m.LoadAndDelete("missing")
	if loaded {
		t.Errorf("LoadAndDelete(missing) loaded = true, want false")
	}
	if v != 0 {
		t.Errorf("LoadAndDelete(missing) zero value = %d, want 0", v)
	}
}

func TestSyncMap_LoadOrStore_New(t *testing.T) {
	var m SyncMap[string, int]
	actual, loaded := m.LoadOrStore("a", 1)
	if loaded {
		t.Error("LoadOrStore on new key should return loaded=false")
	}
	if actual != 1 {
		t.Errorf("LoadOrStore actual = %d, want 1", actual)
	}
}

func TestSyncMap_LoadOrStore_Existing(t *testing.T) {
	var m SyncMap[string, int]
	m.Store("a", 1)

	actual, loaded := m.LoadOrStore("a", 99)
	if !loaded {
		t.Error("LoadOrStore on existing key should return loaded=true")
	}
	if actual != 1 {
		t.Errorf("LoadOrStore actual = %d, want 1 (original value)", actual)
	}
}

func TestSyncMap_Range(t *testing.T) {
	var m SyncMap[string, int]
	m.Store("a", 1)
	m.Store("b", 2)
	m.Store("c", 3)

	seen := make(map[string]int)
	m.Range(func(k string, v int) bool {
		seen[k] = v
		return true
	})

	if len(seen) != 3 {
		t.Errorf("Range visited %d entries, want 3", len(seen))
	}
	for _, k := range []string{"a", "b", "c"} {
		if _, ok := seen[k]; !ok {
			t.Errorf("Range did not visit key %q", k)
		}
	}
}

func TestSyncMap_Range_EarlyStop(t *testing.T) {
	var m SyncMap[string, int]
	m.Store("a", 1)
	m.Store("b", 2)
	m.Store("c", 3)

	count := 0
	m.Range(func(k string, v int) bool {
		count++
		return false // stop after first
	})

	if count != 1 {
		t.Errorf("Range with early stop visited %d entries, want 1", count)
	}
}

func TestSyncMap_PointerValues(t *testing.T) {
	type session struct{ name string }
	var m SyncMap[string, *session]

	s := &session{name: "test"}
	m.Store("s1", s)

	got, ok := m.Load("s1")
	if !ok {
		t.Fatal("Load(s1) returned false")
	}
	if got != s {
		t.Error("Load(s1) returned different pointer")
	}
	if got.name != "test" {
		t.Errorf("got.name = %q, want %q", got.name, "test")
	}
}

func TestSyncMap_ZeroValue(t *testing.T) {
	// SyncMap should be usable without initialization (like sync.Map).
	var m SyncMap[int, string]
	m.Store(1, "one")
	v, ok := m.Load(1)
	if !ok || v != "one" {
		t.Errorf("Load(1) = (%q, %v), want (\"one\", true)", v, ok)
	}
}

func TestSyncMap_ConcurrentAccess(t *testing.T) {
	var m SyncMap[int, int]
	var wg sync.WaitGroup

	// 10 writers storing different keys
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				m.Store(id*100+j, j)
			}
		}(i)
	}

	// 10 readers loading keys
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				m.Load(id*100 + j)
			}
		}(i)
	}

	wg.Wait()

	// Verify all 1000 keys present
	count := 0
	m.Range(func(k, v int) bool {
		count++
		return true
	})
	if count != 1000 {
		t.Errorf("after concurrent writes, count = %d, want 1000", count)
	}
}
