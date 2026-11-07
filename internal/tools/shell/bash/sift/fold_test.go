package sift

import (
	"strings"
	"testing"
)

func TestAFoldedTestRunKeepsWhatTheProgramPrinted(t *testing.T) {
	for _, run := range []struct {
		command, output, tally string
		exit                   int
		printed                []string
	}{
		{"go test -count=1 -run TestTill .",
			"ring 1 250\nring 2 500\nring 3 675\nring 4 1000\n--- FAIL: TestTill (0.00s)\n    till_test.go:11: got 2425, want 2500\nFAIL\nFAIL\tfixture.test/orders\t0.522s\nFAIL\n",
			"go test: ", 1, []string{"ring 3 675"}},
		{"python -m unittest test_till.TillTest.test_till",
			"ring 1 250\nring 2 500\nring 3 675\nring 4 1000\nF\n======================================================================\nFAIL: test_till (test_till.TillTest)\n----------------------------------------------------------------------\nTraceback (most recent call last):\n  File \"E:\\p\\test_till.py\", line 12, in test_till\n    self.assertEqual(till.total, 2500, f\"got {till.total}, want 2500\")\nAssertionError: 2425 != 2500 : got 2425, want 2500\n\n----------------------------------------------------------------------\nRan 1 test in 0.000s\n\nFAILED (failures=1)\n",
			"unittest: 0 passed, 1 failed", 1, []string{"ring 1 250", "ring 3 675", "ring 4 1000"}},
		{"python -m unittest test_till.TillTest.test_till",
			"F\n======================================================================\nFAIL: test_till (test_till.TillTest)\n----------------------------------------------------------------------\nTraceback (most recent call last):\n  File \"E:\\p\\test_till.py\", line 12, in test_till\n    print(\"ring\", 3, 675)\nAssertionError: 2425 != 2500\n\n----------------------------------------------------------------------\nRan 1 test in 0.000s\n\nFAILED (failures=1)\nring 3 675\n",
			"unittest: 0 passed, 1 failed", 1, []string{"ring 3 675"}},
		{"python -m pytest -q test_till.py",
			"F                                                                        [100%]\n=================================== FAILURES ===================================\n__________________________________ test_till ___________________________________\n\n    def test_till():\n>       assert till.total == 2500\nE       assert 2425 == 2500\n\ntest_till.py:9: AssertionError\n----------------------------- Captured stdout call -----------------------------\nring 1 250\nring 2 500\nring 3 675\nring 4 1000\n=========================== short test summary info ============================\nFAILED test_till.py::test_till - assert 2425 == 2500\n1 failed in 0.05s\n",
			"pytest: 0 passed, 1 failed", 1, []string{"ring 3 675", "ring 4 1000"}},
		{"python -m unittest test_till.TillTest.test_till",
			"ring 1 250\n.\n----------------------------------------------------------------------\nRan 1 test in 0.000s\n\nOK\n",
			"unittest: 1 passed, 0 failed", 0, []string{"ring 1 250"}},
		{"python -m unittest test_till.TillTest.test_till",
			"F\n======================================================================\nFAIL: test_till (test_till.TillTest)\n----------------------------------------------------------------------\nTraceback (most recent call last):\n  File \"E:\\p\\test_till.py\", line 12, in test_till\nAssertionError: 2425 != 2500\n\n----------------------------------------------------------------------\nRan 1 test in 0.000s\n\nFAILED (failures=1)\nthe command exited 1\nbash: changed on disk under the project: __pycache__/till.cpython-39.pyc\ncitations: 1 found, 1 resolved\n",
			"unittest: 0 passed, 1 failed", 1, nil},
		{"python -m pytest -q -s test_till.py",
			"ring 1 250\nring 3 675\n.                                                                        [100%]\n1 passed in 0.01s\n",
			"pytest: 1 passed, 0 failed", 0, []string{"ring 1 250", "ring 3 675"}},
		{"go test -v -count=1 -run TestTill .",
			"=== RUN   TestTill\nring 3 675\n--- PASS: TestTill (0.00s)\nPASS\nok  \tfixture.test/orders\t0.4s\n",
			"go test: 1 passed, 0 failed", 0, []string{"ring 3 675"}},
	} {
		folded, known := Fold(Shell{Command: run.command, Stdout: run.output, ExitCode: run.exit})
		if !known {
			t.Errorf("%s was not folded at all", run.command)
			continue
		}
		lines := strings.Split(folded, "\n")
		if !strings.HasPrefix(folded, run.tally) {
			t.Errorf("%s lost its tally:\n%s", run.command, folded)
		}
		for _, want := range run.printed {
			found := false
			for _, line := range lines {
				found = found || line == want || line == "  "+want
			}
			if !found {
				t.Errorf("%s dropped the printed line %q:\n%s", run.command, want, folded)
			}
		}
		if run.printed == nil && strings.Contains(folded, "printed by the program") {
			t.Errorf("%s printed nothing of its own and the fold says it did:\n%s", run.command, folded)
		}
		for _, framework := range []string{"Traceback", "=====", "Captured stdout", "Ran 1 test", "[100%]", "1 passed in", "--- PASS", "=== RUN", "\n  F\n", "\n  PASS\n", "\n  FAIL\n", "ok  \t"} {
			if strings.Contains(folded, framework) {
				t.Errorf("%s kept the framework line %q as if the program printed it:\n%s", run.command, framework, folded)
			}
		}
		t.Logf("%s folded to:\n%s", run.command, folded)
	}
}
