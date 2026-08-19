package workspacestatus

import "testing"

func TestCacheStoreGetLookup(t *testing.T) {
	c := NewCache()
	if _, ok := c.Get("ws1"); ok {
		t.Fatal("empty cache returned a status")
	}
	e := Entry{Status: Status{CIStatus: CISuccess, CIURL: "u", PRNumber: 3, PRURL: "p"}, Branch: "b", HeadSHA: "abc", Terminal: true}
	c.Store("ws1", e)
	got, ok := c.Get("ws1")
	if !ok || got != e.Status {
		t.Errorf("Get = (%+v, %v), want (%+v, true)", got, ok, e.Status)
	}
	le, ok := c.Lookup("ws1")
	if !ok || le.HeadSHA != "abc" || !le.Terminal {
		t.Errorf("Lookup = (%+v, %v)", le, ok)
	}
}

func TestCacheDropExcept(t *testing.T) {
	c := NewCache()
	c.Store("keep", Entry{})
	c.Store("drop", Entry{})
	if !c.DropExcept(map[string]bool{"keep": true}) {
		t.Error("DropExcept should report a removal")
	}
	if _, ok := c.Get("drop"); ok {
		t.Error("dropped entry still present")
	}
	if _, ok := c.Get("keep"); !ok {
		t.Error("kept entry missing")
	}
	if c.DropExcept(map[string]bool{"keep": true}) {
		t.Error("second DropExcept should report no removal")
	}
}

func TestCacheClear(t *testing.T) {
	c := NewCache()
	if c.Clear() {
		t.Error("Clear on empty cache should report false")
	}
	c.Store("ws1", Entry{})
	if !c.Clear() {
		t.Error("Clear should report a removal")
	}
	if _, ok := c.Get("ws1"); ok {
		t.Error("entry survived Clear")
	}
}
