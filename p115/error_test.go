package p115

import (
	"errors"
	"strings"
	"testing"
)

// TestIsTransientP115Error covers the failures worth an immediate retry.
//
// "invalid ec data" comes from the response checksum and turned up on roughly one
// call in ten against a live account; the next attempt succeeded. The transport
// variant comes from the SDK reusing a spent request body, which a fresh call
// avoids entirely.
func TestIsTransientP115Error(t *testing.T) {
	transient := []string{
		"invalid ec data",
		`Post "https://lixian.115.com/lixianssp/?ac=add_task_urls": ` +
			"net/http: HTTP/1.x transport connection broken: http: ContentLength=1840 with Body length 0",
	}
	for _, msg := range transient {
		if !isTransientP115Error(errors.New(msg)) {
			t.Errorf("%q should be retryable", msg)
		}
	}

	permanent := []string{
		"insufficient quota",
		"request failed: 401",
		"connection refused",
		"",
	}
	for _, msg := range permanent {
		if isTransientP115Error(errors.New(msg)) {
			t.Errorf("%q should not be retried", msg)
		}
	}
	if isTransientP115Error(nil) {
		t.Error("nil is not a failure")
	}
}

// TestRetryP115RecoversOnSecondAttempt verifies a transient failure is retried
// and reported as success.
func TestRetryP115RecoversOnSecondAttempt(t *testing.T) {
	calls := 0
	err := retryP115("test", func() error {
		calls++
		if calls == 1 {
			return errors.New("invalid ec data")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("retry should have succeeded, got %v", err)
	}
	if calls != 2 {
		t.Errorf("expected exactly 2 attempts, got %d", calls)
	}
}

// TestRetryP115StopsAfterTwo verifies the retry is bounded.
//
// An infinite retry against a persistently failing endpoint would hang the
// scheduler instead of reporting a failure.
func TestRetryP115StopsAfterTwo(t *testing.T) {
	calls := 0
	err := retryP115("test", func() error {
		calls++
		return errors.New("invalid ec data")
	})
	if err == nil {
		t.Fatal("a persisting failure must be reported")
	}
	if calls != 2 {
		t.Errorf("expected 2 attempts and then give up, got %d", calls)
	}
}

// TestRetryP115DoesNotRetryPermanent verifies real errors are not swallowed by a
// pointless second attempt.
func TestRetryP115DoesNotRetryPermanent(t *testing.T) {
	calls := 0
	err := retryP115("test", func() error {
		calls++
		return errors.New("insufficient quota")
	})
	if err == nil || !strings.Contains(err.Error(), "quota") {
		t.Errorf("the original error should surface, got %v", err)
	}
	if calls != 1 {
		t.Errorf("a permanent error should be attempted once, got %d", calls)
	}
}

// TestRetryP115FirstAttemptSuccess verifies the happy path adds no overhead.
func TestRetryP115FirstAttemptSuccess(t *testing.T) {
	calls := 0
	if err := retryP115("test", func() error { calls++; return nil }); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected a single attempt, got %d", calls)
	}
}

// TestExplainP115ErrorDoesNotBlameSession verifies the wording stays accurate.
//
// A checksum failure is usually transient. Reporting it as "session expired"
// sends the user off to log in again for no reason — which is exactly what
// happened before the retry was added.
func TestExplainP115ErrorDoesNotBlameSession(t *testing.T) {
	got := explainP115Error(errors.New("invalid ec data"))
	if got == nil {
		t.Fatal("nil result")
	}
	if strings.Contains(got.Error(), "invalid ec data") {
		t.Errorf("the raw SDK message should not reach the user: %q", got.Error())
	}
	if strings.Contains(got.Error(), "登录已失效") {
		t.Errorf("a transient glitch must not be reported as an expired session: %q", got.Error())
	}
	if !strings.Contains(got.Error(), "重试") {
		t.Errorf("the message should say a retry already happened: %q", got.Error())
	}
}

// TestExplainP115ErrorMapsAuthFailures verifies genuine auth errors still point
// at signing in.
func TestExplainP115ErrorMapsAuthFailures(t *testing.T) {
	for _, msg := range []string{"request failed: 401", "Unauthorized", "not login", "not logged in"} {
		got := explainP115Error(errors.New(msg))
		if got == nil || !strings.Contains(got.Error(), "登录") {
			t.Errorf("%q should map to a sign-in hint, got %v", msg, got)
		}
	}
}

// TestExplainP115ErrorPassesThroughOthers verifies unrelated failures keep their
// original message, so real diagnostics are not hidden.
func TestExplainP115ErrorPassesThroughOthers(t *testing.T) {
	original := errors.New("connection reset by peer")
	if got := explainP115Error(original); got == nil || got.Error() != original.Error() {
		t.Errorf("unrelated error should be returned unchanged, got %v", got)
	}
}

// TestExplainP115ErrorNil verifies nil stays nil.
func TestExplainP115ErrorNil(t *testing.T) {
	if got := explainP115Error(nil); got != nil {
		t.Errorf("nil should stay nil, got %v", got)
	}
}

// TestExplainP115ErrorIsCaseTolerant verifies the lowercase variants match too.
func TestExplainP115ErrorIsCaseTolerant(t *testing.T) {
	for _, msg := range []string{"NOT LOGIN", "Not Logged In", "UNAUTHORIZED"} {
		if got := explainP115Error(errors.New(msg)); !strings.Contains(got.Error(), "登录") {
			t.Errorf("%q was not recognised, got %q", msg, got.Error())
		}
	}
}
