package validator

import (
	"fmt"
	"regexp"
	"testing"
)

// The regex cache must keep caching (and stay bounded) past its size cap, rather
// than refusing to cache new patterns and recompiling them on every call.
func TestRegexCacheEvictsWhenFull(t *testing.T) {
	regexCacheMu.Lock()
	regexCache = make(map[string]*regexp.Regexp)
	regexCacheMu.Unlock()

	// Insert more distinct patterns than the cap.
	for i := 0; i < maxRegexCacheSize+50; i++ {
		if got := cachedRegex(fmt.Sprintf("prefix%d_[a-z]+", i)); got == nil {
			t.Fatalf("cachedRegex returned nil for a valid pattern (i=%d)", i)
		}
	}

	regexCacheMu.RLock()
	n := len(regexCache)
	regexCacheMu.RUnlock()
	if n > maxRegexCacheSize {
		t.Fatalf("cache grew past cap: %d > %d", n, maxRegexCacheSize)
	}

	// A freshly inserted pattern is still cached (not dropped like past-cap
	// patterns used to be).
	last := fmt.Sprintf("prefix%d_[a-z]+", maxRegexCacheSize+49)
	_ = cachedRegex(last)
	regexCacheMu.RLock()
	_, ok := regexCache[last]
	regexCacheMu.RUnlock()
	if !ok {
		t.Fatal("newly compiled pattern was not cached")
	}
}
