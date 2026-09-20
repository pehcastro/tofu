package rule

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixturePackage(name string) string {
	return filepath.Join("testdata", "tree", name)
}

func packageFindings(t *testing.T, checker, pkg string) []Finding {
	t.Helper()
	r := Rule{ID: checker, Kind: KindStructural, Checker: checker, Mode: ModeShadow}
	dir := fixturePackage(pkg)
	fire, err := Run(r, Builtins(), GoPackage{Dir: dir}, dir, time.Now())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return fire.Findings
}

func TestTestAssertionFiresOnATestWhoseClaimsAreAllNilOrZeroAndNotOnOneThatComparesValues(t *testing.T) {
	fired := packageFindings(t, "test_assertion", "weak")
	if len(fired) != 1 {
		t.Fatalf("the rule fired %d times on the weak package, want 1: %+v", len(fired), fired)
	}
	if !strings.HasSuffix(fired[0].Target, "weak_test.go:5") {
		t.Fatalf("the finding points at %q, want the test that only claims err is nil and Total is not zero", fired[0].Target)
	}
	if !strings.Contains(fired[0].Detail, "TestSummariseReturnsAReport") {
		t.Fatalf("the detail does not name the test function: %q", fired[0].Detail)
	}
	if silent := packageFindings(t, "test_assertion", "strong"); len(silent) != 0 {
		t.Fatalf("the rule fired on tests that compare against a wanted value: %+v", silent)
	}
}

func TestTestAssertionFiresOnAnAssertionLibraryCallThatOnlyChecksExistence(t *testing.T) {
	fired := packageFindings(t, "test_assertion", "mocked")
	if len(fired) != 1 {
		t.Fatalf("the rule fired %d times on a test whose only claim is assert.NotNil, want 1: %+v", len(fired), fired)
	}
}

func TestTestMockBoundaryFiresOnAStubbedPackageVariableAndOnACallAssertionAndNotOnAPlainTest(t *testing.T) {
	stubbed := packageFindings(t, "test_mock_boundary", "weak")
	if len(stubbed) != 1 {
		t.Fatalf("the rule fired %d times on a test that reassigns the package variable scale, want 1: %+v", len(stubbed), stubbed)
	}
	if !strings.Contains(stubbed[0].Detail, "scale") {
		t.Fatalf("the detail does not name the stubbed helper: %q", stubbed[0].Detail)
	}
	called := packageFindings(t, "test_mock_boundary", "mocked")
	if len(called) != 1 {
		t.Fatalf("the rule fired %d times on an AssertCalled, want 1: %+v", len(called), called)
	}
	if silent := packageFindings(t, "test_mock_boundary", "strong"); len(silent) != 0 {
		t.Fatalf("the rule fired on a test that mocks nothing: %+v", silent)
	}
}

func TestTestBoundaryCasesFiresOnAFileWithNoEmptyOrZeroCaseAndNotOnOneThatHasBoth(t *testing.T) {
	fired := packageFindings(t, "test_boundary_cases", "weak")
	if len(fired) != 1 {
		t.Fatalf("the rule fired %d times on a file that passes no nil, zero or empty input, want 1: %+v", len(fired), fired)
	}
	if !strings.HasSuffix(fired[0].Target, "weak_test.go") {
		t.Fatalf("the finding points at %q, want the test file", fired[0].Target)
	}
	if silent := packageFindings(t, "test_boundary_cases", "strong"); len(silent) != 0 {
		t.Fatalf("the rule fired on a file that passes zero and a negative value: %+v", silent)
	}
}
