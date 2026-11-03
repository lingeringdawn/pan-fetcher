package p115

import (
	"errors"
	"strings"
	"testing"
)

// TestExplainP115ErrorMapsExpiredSession covers the message that prompted this.
//
// The SDK reports a CRC mismatch while decrypting a response as "invalid ec
// data". That happens when the session has expired and 115 answers with a login
// page instead of encrypted data. Passed through verbatim it tells the user
// nothing actionable, so it is mapped to a hint about signing in again.
func TestExplainP115ErrorMapsExpiredSession(t *testing.T) {
	got := explainP115Error(errors.New("invalid ec data"))
	if got == nil {
		t.Fatal("nil result for a real error")
	}
	if strings.Contains(got.Error(), "invalid ec data") {
		t.Errorf("the raw SDK message should not reach the user: %q", got.Error())
	}
	if !strings.Contains(got.Error(), "登录") {
		t.Errorf("message should point at signing in, got %q", got.Error())
	}
}

// TestExplainP115ErrorMapsAuthFailures covers the other auth-shaped errors.
func TestExplainP115ErrorMapsAuthFailures(t *testing.T) {
	for _, msg := range []string{
		"request failed: 401",
		"Unauthorized",
		"not login",
		"not logged in",
	} {
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
	got := explainP115Error(original)
	if got == nil || got.Error() != original.Error() {
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
//
// The SDK and 115 do not agree on capitalisation across endpoints.
func TestExplainP115ErrorIsCaseTolerant(t *testing.T) {
	for _, msg := range []string{"NOT LOGIN", "Not Logged In", "UNAUTHORIZED"} {
		if got := explainP115Error(errors.New(msg)); !strings.Contains(got.Error(), "登录") {
			t.Errorf("%q was not recognised, got %q", msg, got.Error())
		}
	}
}

// TestExplainP115ErrorWrapsInContext verifies callers can still wrap it without
// losing the hint.
func TestExplainP115ErrorWrapsInContext(t *testing.T) {
	inner := explainP115Error(errors.New("invalid ec data"))
	wrapped := errors.New("add magnet failed: " + inner.Error())
	if !strings.Contains(wrapped.Error(), "登录") {
		t.Errorf("the hint should survive wrapping: %q", wrapped.Error())
	}
	if strings.Contains(wrapped.Error(), "invalid ec data") {
		t.Errorf("the raw message should be gone: %q", wrapped.Error())
	}
}
