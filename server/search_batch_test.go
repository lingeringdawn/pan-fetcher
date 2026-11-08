package server

import (
	"strings"
	"testing"
)

// TestSearchResultsUseCheckboxes verifies the results table offers multi-select
// instead of a per-row add button.
//
// Adding one release at a time meant repeating the folder-picker dialog for every
// item; a checkbox column plus one action on the toolbar replaces that.
func TestSearchResultsUseCheckboxes(t *testing.T) {
	src := readSource(t, templateSourceFile)

	if strings.Contains(src, "addTaskWithBrowse") {
		t.Error("the per-row add button should be gone")
	}
	for _, want := range []string{
		`class="result-check"`,
		`id="select-all-results"`,
		`id="batch-add-btn"`,
		`id="batch-count"`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("missing batch UI element: %s", want)
		}
	}
}

// TestBatchSelectionWired verifies the handlers referenced by the markup exist.
//
// The inline onchange/onclick attributes are plain strings, so a renamed function
// breaks the page silently — nothing fails to compile.
func TestBatchSelectionWired(t *testing.T) {
	src := readSource(t, templateSourceFile)

	for _, fn := range []string{
		"function toggleSelectAllResults(",
		"function updateSelectedCount(",
		"function addSelectedTasks(",
		"function doAddTasks(",
		"function selectedResults(",
	} {
		if !strings.Contains(src, fn) {
			t.Errorf("missing handler: %s", fn)
		}
	}
	// The markup has to call them.
	for _, call := range []string{
		`onchange="toggleSelectAllResults(this.checked)"`,
		`onchange="updateSelectedCount()"`,
		`onclick="addSelectedTasks()"`,
	} {
		if !strings.Contains(src, call) {
			t.Errorf("markup does not call the handler: %s", call)
		}
	}
}

// TestDynamicRowsCarryCheckbox verifies the client-side row builder agrees with
// the server-rendered table.
//
// Rows are rebuilt in JS when paging through cached results; if that path kept the
// old button the feature would only work on the first page.
func TestDynamicRowsCarryCheckbox(t *testing.T) {
	src := readSource(t, templateSourceFile)

	i := strings.Index(src, "function buildRowHTML(")
	if i < 0 {
		t.Fatal("buildRowHTML not found")
	}
	end := i + 1500
	if end > len(src) {
		end = len(src)
	}
	body := src[i:end]

	if !strings.Contains(body, "result-check") {
		t.Error("dynamically built rows should include the checkbox")
	}
	if strings.Contains(body, "addTaskWithBrowse") {
		t.Error("dynamically built rows should no longer use the add button")
	}
}

// TestBatchSubmitUsesNewline verifies multiple magnets are sent in the format the
// server splits on.
//
// decodeOfflineTask hands the form value to parseTasks, which splits on newline,
// comma and semicolon. A magnet's dn= parameter is URL-encoded, so a newline is
// the only separator that cannot appear inside one.
func TestBatchSubmitUsesNewline(t *testing.T) {
	src := readSource(t, templateSourceFile)
	if !strings.Contains(src, `pendingMagnets.join('\n')`) {
		t.Error("bulk submit should join magnets with newlines")
	}
}

// TestParseTasksSplitsBatch documents the server side of that contract.
func TestParseTasksSplitsBatch(t *testing.T) {
	got := parseTasks("magnet:?xt=urn:btih:aaa\nmagnet:?xt=urn:btih:bbb\n\nmagnet:?xt=urn:btih:ccc")
	if len(got) != 3 {
		t.Fatalf("want 3 tasks, got %d: %v", len(got), got)
	}
	for i, want := range []string{"aaa", "bbb", "ccc"} {
		if !strings.Contains(got[i], want) {
			t.Errorf("task %d = %q", i, got[i])
		}
	}
}

// TestBatchI18nPresent verifies the new labels exist in both languages.
func TestBatchI18nPresent(t *testing.T) {
	src := readSource(t, "i18n.go")
	for _, key := range []string{
		"batch_none_selected", "batch_selected", "batch_add_btn", "batch_added", "select_all",
	} {
		if n := strings.Count(src, `"`+key+`"`); n != 2 {
			t.Errorf("key %q appears %d times, want 2 (zh + en)", key, n)
		}
	}
}
