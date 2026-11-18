// Package qa holds regression tests for bugs a QA pass filed that are not
// fixed yet.
//
// Every QA issue gets a test that checks the right behaviour and names the
// issue. While the issue is open, its test is wrapped in KnownBug:
//
//	func TestRefundReplay(t *testing.T) {
//		// Regression: toskar-core#1258
//		qa.KnownBug(t, "toskar-core#1258", func(t testing.TB) {
//			... the checks that fail today ...
//		})
//	}
//
// KnownBug runs the checks every time. While they fail, the test is skipped
// with what failed, so CI stays green. Once they pass, the test fails with
// "toskar-core#1258 looks fixed", so the fix must unwrap it into a plain test
// in the same change, and from then on the bug can't come back unnoticed.
// The QA ledger (.github/scripts/qa-ledger.sh) finds the test by the issue
// it names.
package qa

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// KnownBug runs check, which should pass once issue is fixed.
func KnownBug(t *testing.T, issue string, check func(t testing.TB)) {
	t.Helper()
	r := &recorder{TB: t}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() {
			if p := recover(); p != nil {
				r.fail(fmt.Sprintf("panic: %v", p))
			}
		}()
		check(r)
	}()
	<-done
	if r.Failed() {
		t.Skipf("known bug %s, still open:\n%s", issue, strings.Join(r.messages(), "\n"))
	}
	t.Fatalf("%s looks fixed: these checks pass now. Unwrap them from qa.KnownBug into a plain test, keeping the Regression: %s comment, in the change that fixes it.", issue, issue)
}

// recorder is a testing.TB whose failures are collected instead of failing
// the test. Cleanup, TempDir, Setenv and the rest go to the real test.
type recorder struct {
	testing.TB

	mu     sync.Mutex
	failed bool
	msgs   []string
}

func (r *recorder) fail(msg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failed = true
	if msg != "" {
		r.msgs = append(r.msgs, msg)
	}
}

func (r *recorder) messages() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.msgs...)
}

func (r *recorder) Helper() {}
func (r *recorder) Fail()   { r.fail("") }
func (r *recorder) Failed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.failed
}
func (r *recorder) FailNow()                  { r.fail(""); runtime.Goexit() }
func (r *recorder) Error(args ...any)         { r.fail(fmt.Sprint(args...)) }
func (r *recorder) Errorf(f string, a ...any) { r.fail(fmt.Sprintf(f, a...)) }
func (r *recorder) Fatal(args ...any)         { r.fail(fmt.Sprint(args...)); runtime.Goexit() }
func (r *recorder) Fatalf(f string, a ...any) { r.fail(fmt.Sprintf(f, a...)); runtime.Goexit() }
func (r *recorder) Skip(args ...any)          { r.fail("skipped: " + fmt.Sprint(args...)); runtime.Goexit() }
func (r *recorder) Skipf(f string, a ...any) {
	r.fail("skipped: " + fmt.Sprintf(f, a...))
	runtime.Goexit()
}
func (r *recorder) SkipNow() { r.fail("skipped"); runtime.Goexit() }
