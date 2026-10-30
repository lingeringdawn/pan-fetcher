package server

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// --- Enabled defaults ---

// TestEnabledMissingMeansEnabled pins the compatibility rule for subscription
// files written before the flag existed.
//
// v1.4.1 stored it as *bool and read nil as true. The plain-bool rewrite made the
// zero value false, so every legacy entry silently stopped running after an
// upgrade, with nothing in the log to explain it.
func TestEnabledMissingMeansEnabled(t *testing.T) {
	cases := []struct {
		name string
		json string
		want bool
	}{
		{"missing", `{"name":"a","url":"/rss/search?q=1","cid":"1"}`, true},
		{"explicit true", `{"name":"a","url":"/rss/search?q=1","cid":"1","enabled":true}`, true},
		{"explicit false", `{"name":"a","url":"/rss/search?q=1","cid":"1","enabled":false}`, false},
	}
	for _, c := range cases {
		var e rssFeedEntry
		if err := json.Unmarshal([]byte(c.json), &e); err != nil {
			t.Fatalf("%s: unmarshal: %v", c.name, err)
		}
		if e.Enabled != c.want {
			t.Errorf("%s: Enabled = %v, want %v", c.name, e.Enabled, c.want)
		}
	}
}

// TestEnabledSurvivesRoundTrip verifies a decoded entry re-encodes explicitly, so
// the legacy shape is normalised after the first read.
func TestEnabledSurvivesRoundTrip(t *testing.T) {
	var e rssFeedEntry
	if err := json.Unmarshal([]byte(`{"name":"a","url":"/x","cid":"1"}`), &e); err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"enabled":true`) {
		t.Errorf("re-encoded entry should carry an explicit enabled flag: %s", out)
	}
}

// TestEnabledPreservesOtherFields verifies the custom decoder did not drop or
// mangle the remaining fields.
func TestEnabledPreservesOtherFields(t *testing.T) {
	raw := `{"name":"番剧","url":"/rss/search?q=x","cid":"99","savepath":"目录","filter":"group:NEST"}`
	var e rssFeedEntry
	if err := json.Unmarshal([]byte(raw), &e); err != nil {
		t.Fatal(err)
	}
	if e.Name != "番剧" || e.URL != "/rss/search?q=x" || e.Cid != "99" ||
		e.SavePath != "目录" || e.Filter != "group:NEST" {
		t.Errorf("fields lost in custom decode: %+v", e)
	}
}

// --- readRssFeedsErr ---

// TestReadRssFeedsReportsCorruption verifies a broken file is an error, not an
// empty list.
//
// The old reader returned an empty map for every failure, so a corrupt file was
// reported by the scheduler as "no subscriptions" — the one message that hides
// the problem.
func TestReadRssFeedsReportsCorruption(t *testing.T) {
	saved := rssJsonPath
	t.Cleanup(func() { rssJsonPath = saved })
	dir := t.TempDir()

	// Missing file: normal on first run, not an error.
	rssJsonPath = dir + "/missing.json"
	feeds, err := (&Server{}).readRssFeedsErr()
	if err != nil {
		t.Errorf("missing file should not be an error: %v", err)
	}
	if len(feeds) != 0 {
		t.Errorf("missing file: want empty, got %d", len(feeds))
	}

	// Corrupt JSON: an error the caller must surface.
	bad := dir + "/bad.json"
	if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	rssJsonPath = bad
	if _, err := (&Server{}).readRssFeedsErr(); err == nil {
		t.Error("corrupt file should be reported as an error")
	}

	// Valid file parses.
	good := dir + "/good.json"
	if err := os.WriteFile(good, []byte(`{"s":[{"name":"n","url":"/u","cid":"1"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	rssJsonPath = good
	feeds, err = (&Server{}).readRssFeedsErr()
	if err != nil {
		t.Fatalf("valid file: %v", err)
	}
	if len(feeds["s"]) != 1 {
		t.Errorf("valid file: want 1 entry, got %d", len(feeds["s"]))
	}
}

// --- retry backoff ---

// TestRetryBackoffGrows verifies the backoff starts small and grows, then caps.
func TestRetryBackoffGrows(t *testing.T) {
	want := []time.Duration{
		5 * time.Minute,  // 1st failure
		10 * time.Minute, // 2nd
		20 * time.Minute, // 3rd
		40 * time.Minute, // 4th
		40 * time.Minute, // capped
		40 * time.Minute, // stays capped
	}
	for i, w := range want {
		if got := retryBackoff(i + 1); got != w {
			t.Errorf("retryBackoff(%d) = %v, want %v", i+1, got, w)
		}
	}
	// Degenerate input must not panic or return zero.
	if got := retryBackoff(0); got <= 0 {
		t.Errorf("retryBackoff(0) = %v, want a positive duration", got)
	}
	if got := retryBackoff(-3); got <= 0 {
		t.Errorf("retryBackoff(-3) = %v, want a positive duration", got)
	}
}

// --- concurrency guard ---

// TestRunSubscriptionRejectsConcurrentRun verifies the same subscription cannot be
// started twice at once.
//
// ProcessRSSFeed checks the dedup cache, submits, and only then records hashes, so
// two overlapping runs would both pass the check and submit the same releases.
func TestRunSubscriptionRejectsConcurrentRun(t *testing.T) {
	saved := subsRunning
	t.Cleanup(func() { subsRunning = saved })
	subsRunning = make(map[string]bool)

	// Simulate a run already in progress for this key.
	subsRunning["番剧"] = true

	srv := &Server{Agent: &fakeServerAgent{}}
	_, err := srv.runSubscription("番剧", "/rss/search?q=x", "1", "", "")
	if !errors.Is(err, errSubsAlreadyRunning) {
		t.Errorf("want errSubsAlreadyRunning, got %v", err)
	}
}

// TestRunSubscriptionReleasesLock verifies the guard is cleared after a run, so a
// failure does not wedge the subscription forever.
func TestRunSubscriptionReleasesLock(t *testing.T) {
	saved := subsRunning
	t.Cleanup(func() { subsRunning = saved })
	subsRunning = make(map[string]bool)

	srv := &Server{Agent: &fakeServerAgent{}}
	if _, err := srv.runSubscription("k", "/rss/search?q=x", "1", "", ""); err != nil {
		t.Fatalf("first run: %v", err)
	}
	subsRunMu.Lock()
	still := subsRunning["k"]
	subsRunMu.Unlock()
	if still {
		t.Error("runSubscription left the key marked as running")
	}
	// A second run must be accepted.
	if _, err := srv.runSubscription("k", "/rss/search?q=x", "1", "", ""); err != nil {
		t.Errorf("second run should be accepted: %v", err)
	}
}

// --- failure is reported ---

// failingAgent reports a fetch failure, the way the real client now does.
type failingAgent struct {
	*fakeServerAgent
}

func (f *failingAgent) ProcessRSSFeed(url, cid, savepath, subKey string) ([]string, error) {
	return nil, errors.New("fetch RSS failed")
}

// TestRunSubscriptionPropagatesFailure verifies the error reaches the caller.
//
// Without it a broken feed is indistinguishable from a quiet one, so the
// scheduler could never retry the broken ones.
func TestRunSubscriptionPropagatesFailure(t *testing.T) {
	saved := subsRunning
	t.Cleanup(func() { subsRunning = saved })
	subsRunning = make(map[string]bool)

	srv := &Server{Agent: &failingAgent{&fakeServerAgent{}}}
	_, err := srv.runSubscription("k", "/rss/search?q=x", "1", "", "")
	if err == nil {
		t.Fatal("a failed fetch must be reported as an error")
	}
	if errors.Is(err, errSubsAlreadyRunning) {
		t.Fatal("a fetch failure must not be reported as 'already running'")
	}
}

// TestRunSubscriptionRequiresLogin verifies an unauthenticated server reports it
// instead of silently doing nothing.
func TestRunSubscriptionRequiresLogin(t *testing.T) {
	saved := subsRunning
	t.Cleanup(func() { subsRunning = saved })
	subsRunning = make(map[string]bool)

	if _, err := (&Server{}).runSubscription("k", "/x", "1", "", ""); err == nil {
		t.Error("want an error when 115 is not logged in")
	}
}

// --- scheduler shape ---

// TestSchedulerUsesRealBackoff guards the regression where the retry map was
// declared, read and deleted but never populated.
func TestSchedulerUsesRealBackoff(t *testing.T) {
	body := schedulerBody(t)

	if !strings.Contains(body, "retryBackoff(") {
		t.Error("the scheduler should use retryBackoff for failed feeds")
	}
	if !strings.Contains(body, "st.failures++") {
		t.Error("the scheduler should count consecutive failures")
	}
	if !strings.Contains(body, "st.nextRun = time.Now().Add(") {
		t.Error("the scheduler should schedule the next run per subscription")
	}
	// The old dead map must be gone.
	if strings.Contains(body, "retryMap") {
		t.Error("retryMap was never populated; it should not come back")
	}
}

// TestSchedulerWarnsOnMissingCid verifies a subscription without a target folder
// is reported rather than silently skipped.
func TestSchedulerWarnsOnMissingCid(t *testing.T) {
	if !strings.Contains(schedulerBody(t), "未设置保存目录") {
		t.Error("the scheduler should log subscriptions that cannot run for lack of a cid")
	}
}

// TestSchedulerDistinguishesReadFailure verifies a broken config file is not
// reported as "no subscriptions".
func TestSchedulerDistinguishesReadFailure(t *testing.T) {
	body := schedulerBody(t)
	if !strings.Contains(body, "readRssFeedsErr()") {
		t.Error("the scheduler should use readRssFeedsErr so a broken file is not silently empty")
	}
	if !strings.Contains(body, "订阅配置读取失败") {
		t.Error("the scheduler should log the read failure")
	}
}

// funcBody returns the body of the named function, from its opening brace to the
// matching close.
//
// A plain search for "\n}\n" is not good enough: it can match a brace in an
// unrelated function further down the file, which makes assertions silently check
// the wrong code — and pass. Braces are counted instead.
func funcBody(t *testing.T, src, signature string) string {
	t.Helper()
	i := strings.Index(src, signature)
	if i < 0 {
		t.Fatalf("%s not found", signature)
	}
	open := strings.Index(src[i:], "{")
	if open < 0 {
		t.Fatalf("%s has no body", signature)
	}
	start := i + open
	depth := 0
	for j := start; j < len(src); j++ {
		switch src[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[start : j+1]
			}
		}
	}
	t.Fatalf("%s has an unbalanced body", signature)
	return ""
}

func schedulerBody(t *testing.T) string {
	t.Helper()
	return funcBody(t, readSource(t, "server.go"), "func (s *Server) autoRunSubscriptions()")
}

// TestFuncBodyStaysInsideFunction verifies the helper really bounds the body.
//
// The previous version searched for the next "\n}\n", which could land in a
// different function; the assertions then checked code that was not under test.
func TestFuncBodyStaysInsideFunction(t *testing.T) {
	src := readSource(t, "server.go")
	body := funcBody(t, src, "func (s *Server) autoCheckUpdates()")

	// This function's own markers.
	if !strings.Contains(body, "latestReleaseTag()") {
		t.Error("body should contain the function's own call")
	}
	// Markers that belong to functions further down must not be present.
	for _, foreign := range []string{"runSubscription(", "cycle :=", "subsRunning"} {
		if strings.Contains(body, foreign) {
			t.Errorf("body leaked into another function (found %q)", foreign)
		}
	}
}

// TestEntryPointsShareOneRunner verifies the three paths that run a subscription
// all go through runSubscription.
//
// Keeping separate copies is what let the scheduler drift: it had its own dedup
// accounting, its own notification call, and a failure path that could never fire.
func TestEntryPointsShareOneRunner(t *testing.T) {
	src := readSource(t, "server.go")

	if n := strings.Count(src, "s.runSubscription("); n != 3 {
		t.Errorf("expected 3 callers of runSubscription (scheduler, run-now, quick start), found %d", n)
	}
	// No direct calls should remain outside runSubscription itself.
	if n := strings.Count(src, "Agent.ProcessRSSFeed("); n != 1 {
		t.Errorf("expected 1 direct ProcessRSSFeed call (inside runSubscription), found %d", n)
	}
}

// TestUpdateIntervalIndependentOfSubsInterval verifies the update poll no longer
// borrows the subscription interval.
func TestUpdateIntervalIndependentOfSubsInterval(t *testing.T) {
	body := funcBody(t, readSource(t, "server.go"), "func (s *Server) autoCheckUpdates()")

	// Only real code counts: the comment there explains why the setting is no
	// longer used, which would otherwise match.
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		if strings.Contains(trimmed, "SubsInterval") {
			t.Errorf("the update poll should not read SubsInterval: editing the subscription interval must not change how often updates are checked: %s", trimmed)
		}
	}
}
