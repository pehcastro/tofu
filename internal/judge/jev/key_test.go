package jev

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"tofu/internal/sys"
	"tofu/internal/transport"
)

func keyName() string { return "OPENROUTER" + "_KEY" }

func mustAbs(t *testing.T, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("resolving %s: %v", path, err)
	}
	return abs
}

func TestKeyPrefersTheEnvironment(t *testing.T) {
	t.Setenv(keyName(), "from-the-environment")
	got, err := Key("")
	if err != nil {
		t.Fatalf("reading the key: %v", err)
	}
	if got != "from-the-environment" {
		t.Fatalf("expected the environment value, got a value of length %d", len(got))
	}
}

func freshHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	return home
}

func TestKeyFindsTheDatabaseBeforeTheEnvironmentAndTheFile(t *testing.T) {
	freshHome(t)
	t.Setenv(keyName(), "from-the-environment")
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte(keyName()+"=from-the-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := sys.SaveKey(keyName(), "from-the-database"); err != nil {
		t.Fatalf("storing the key: %v", err)
	}
	got, err := Key(path)
	if err != nil || got != "from-the-database" {
		t.Fatalf("expected the database value, got a value of length %d and %v", len(got), err)
	}
	located, err := Locate(path)
	if err != nil || located.Source != SourceDatabase {
		t.Fatalf("expected SourceDatabase, got %v and %v", located.Source, err)
	}
}

func TestReadingTheKeyCreatesNoDatabase(t *testing.T) {
	home := freshHome(t)
	t.Setenv(keyName(), "from-the-environment")
	if _, err := Key(""); err != nil {
		t.Fatal(err)
	}
	stored := filepath.Join(sys.StateDir(home), sys.CredentialStoreName)
	if _, err := os.Stat(stored); !os.IsNotExist(err) {
		t.Fatalf("reading the key left %s behind: %v", stored, err)
	}
}

func TestWithNoHomeTheEnvironmentKeyIsFoundAndNoDatabaseIsOpened(t *testing.T) {
	temp := t.TempDir()
	t.Setenv("TEMP", temp)
	t.Setenv("TMP", temp)
	t.Setenv("USERPROFILE", "")
	t.Setenv("HOME", "")
	t.Setenv(keyName(), "made-up-environment-key")
	located, err := Locate("")
	if err != nil || located.Source != SourceEnvironment {
		t.Fatalf("expected SourceEnvironment with no home, got %v and %v", located.Source, err)
	}
	if err := filepath.WalkDir(temp, func(path string, entry fs.DirEntry, err error) error {
		if err == nil && entry.Name() == sys.CredentialStoreName {
			t.Errorf("reading the key with no home opened %s", path)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestKeyFallsBackToTheEnvFile(t *testing.T) {
	t.Setenv(keyName(), "")
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	body := "# a comment\n\nOTHER=1\nexport " + keyName() + "=\"from-the-file\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	got, err := Key(path)
	if err != nil {
		t.Fatalf("reading the key: %v", err)
	}
	if got != "from-the-file" {
		t.Fatalf("expected the file value, got a value of length %d", len(got))
	}
}

func TestKeyFailsWhenThereIsNone(t *testing.T) {
	t.Setenv(keyName(), "")
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("OTHER=1\n"), 0o600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	_, err := Key(path)
	if transport.KindOf(err) != transport.KindMissingCredential {
		t.Fatalf("expected kind missing_credential, got %v", err)
	}
	if !transport.KindOf(err).Fatal() {
		t.Fatal("a missing credential must be fatal")
	}
}

func TestLocateFromTheEnvironment(t *testing.T) {
	t.Setenv(keyName(), "from-the-environment")
	got, err := Locate("")
	if err != nil {
		t.Fatalf("locating the key: %v", err)
	}
	if got.Source != SourceEnvironment {
		t.Fatalf("expected SourceEnvironment, got %v", got.Source)
	}
	if got.Length != len("from-the-environment") {
		t.Fatalf("expected the length to match, got %d", got.Length)
	}
}

func TestLocateFromTheEnvFile(t *testing.T) {
	t.Setenv(keyName(), "")
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	body := keyName() + "=from-the-file\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	got, err := Locate(path)
	if err != nil {
		t.Fatalf("locating the key: %v", err)
	}
	if got.Source != SourceDotEnv {
		t.Fatalf("expected SourceDotEnv, got %v", got.Source)
	}
	want := mustAbs(t, path)
	if got.Path != want {
		t.Fatalf("expected the resolved path %s, got %s", want, got.Path)
	}
	if got.Length != len("from-the-file") {
		t.Fatalf("expected the length to match, got %d", got.Length)
	}
}

func TestLocateWhenThereIsNone(t *testing.T) {
	t.Setenv(keyName(), "")
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("OTHER=1\n"), 0o600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	got, err := Locate(path)
	if err == nil {
		t.Fatal("expected the missing key to be refused")
	}
	if got.Source != SourceMissing {
		t.Fatalf("expected SourceMissing, got %v", got.Source)
	}
	want := mustAbs(t, path)
	if got.Path != want {
		t.Fatalf("expected the resolved path %s, got %s", want, got.Path)
	}
}

func TestATestThatDoesNotOptInReachesNoCredentialInTheSourceTree(t *testing.T) {
	t.Setenv(keyName(), "")
	t.Chdir(sys.SourceRoot())
	key, err := Key(".env")
	if key != "" {
		t.Fatalf("a test read a credential of length %d out of the source tree", len(key))
	}
	if transport.KindOf(err) != transport.KindMissingCredential {
		t.Fatalf("expected kind missing_credential, got %v", err)
	}
}

func TestATestThatDoesNotOptInReachesNoCredentialInTheHomeStateDirectory(t *testing.T) {
	t.Setenv(keyName(), "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("skipped, not counted as a pass: this machine reports no home directory (%v)", err)
	}
	if strings.HasPrefix(home, os.TempDir()) {
		t.Skipf("skipped, not counted as a pass: the home directory %s is a temporary one, so it is nobody's credential store", home)
	}
	path := filepath.Join(sys.StateDir(home), ".env")
	key, keyErr := Key(path)
	if key != "" {
		t.Fatalf("a test read a credential of length %d out of the home state directory", len(key))
	}
	if keyErr == nil || !strings.Contains(keyErr.Error(), "hidden from tests") {
		t.Fatalf("expected %s to be hidden from tests, got %v", path, keyErr)
	}
}

func TestATestThatDoesNotOptInReachesNoCredentialItPlantedFromTheOwnersOwn(t *testing.T) {
	if !sys.PlantOwnerCredential(t, keyName()) {
		t.Skipf("skipped, not counted as a pass: this machine carries no %s in the source tree .env", keyName())
	}
	key, err := Key("")
	if key != "" {
		t.Fatalf("a test read the owner credential of length %d out of the environment", len(key))
	}
	if transport.KindOf(err) != transport.KindMissingCredential {
		t.Fatalf("expected kind missing_credential, got %v", err)
	}
}

func TestATestThatOptsInByNameReachesTheCredentialAgain(t *testing.T) {
	sys.AllowLiveCredential(t)
	t.Setenv(keyName(), "")
	root := sys.SourceRoot()
	if _, err := os.Stat(filepath.Join(root, ".env")); err != nil {
		t.Skipf("skipped, not counted as a pass: this machine has no .env at %s (%v)", root, err)
	}
	t.Chdir(root)
	key, err := Key(".env")
	if err != nil {
		t.Fatalf("an opted-in test was refused: %v", err)
	}
	if key == "" {
		t.Fatal("an opted-in test read an empty credential")
	}
}

func keyErrorLines(t *testing.T) (paths, lines []string) {
	t.Helper()
	t.Setenv(keyName(), "")
	dir := t.TempDir()
	unnamed := filepath.Join(dir, "unnamed", ".env")
	unreadable := filepath.Join(dir, "unreadable", ".env")
	if err := os.MkdirAll(filepath.Dir(unnamed), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unnamed, []byte("OTHER=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(unreadable, 0o755); err != nil {
		t.Fatal(err)
	}
	paths = []string{filepath.Join(dir, "gone", ".env"), unnamed, unreadable}
	for _, path := range paths {
		_, err := Key(path)
		if err == nil {
			t.Fatalf("%s was accepted as a key", path)
		}
		lines = append(lines, err.Error())
	}
	return paths, lines
}

func TestTheKeyErrorLineIsWordForWordWhatItWas(t *testing.T) {
	paths, lines := keyErrorLines(t)
	head := "jev.Key: missing_credential: "
	want := []string{
		head + keyName() + " is not set and " + paths[0] + " does not exist",
		head + keyName() + " is not set and " + paths[1] + " does not carry it",
		head + "reading " + paths[2] + ": ",
	}
	if lines[0] != want[0] || lines[1] != want[1] {
		t.Errorf("a log line changed:\ngot  %q\nwant %q\ngot  %q\nwant %q", lines[0], want[0], lines[1], want[1])
	}
	if !strings.HasPrefix(lines[2], want[2]) {
		t.Errorf("the unreadable line reads %q, want it to start %q", lines[2], want[2])
	}
}

func TestNobodyOutsideTheJudgeMatchesTheKeyErrorText(t *testing.T) {
	_, lines := keyErrorLines(t)
	root := sys.SourceRoot()
	judge := filepath.Join(root, "internal", "judge")
	var found []string
	for _, area := range []string{"bench", "cmd", "interface", "internal", "library"} {
		if err := filepath.WalkDir(filepath.Join(root, area), func(path string, entry fs.DirEntry, err error) error {
			switch {
			case err != nil:
				return err
			case entry.IsDir() && path == judge:
				return fs.SkipDir
			case entry.IsDir() || !strings.HasSuffix(path, ".go"):
				return nil
			}
			matched, matchErr := matchesOn(path, lines)
			found = append(found, matched...)
			return matchErr
		}); err != nil {
			t.Fatalf("walking %s: %v", area, err)
		}
	}
	if len(found) > 0 {
		t.Fatalf("%d places read this package's error text instead of its typed reason:\n%s", len(found), strings.Join(found, "\n"))
	}
}

func matchesOn(path string, lines []string) ([]string, error) {
	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var found []string
	ast.Inspect(parsed, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || !isTextMatcher(call.Fun) {
			return true
		}
		for _, argument := range call.Args {
			literal, ok := argument.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				continue
			}
			text, unquoteErr := strconv.Unquote(literal.Value)
			if unquoteErr != nil || len(strings.Fields(text)) < 2 {
				continue
			}
			for _, line := range lines {
				if strings.Contains(line, text) {
					found = append(found, path+":"+strconv.Itoa(fileSet.Position(literal.Pos()).Line)+" matches "+literal.Value)
					break
				}
			}
		}
		return true
	})
	return found, nil
}

func isTextMatcher(fun ast.Expr) bool {
	selector, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := selector.X.(*ast.Ident)
	return ok && (pkg.Name == "strings" || pkg.Name == "bytes" || pkg.Name == "regexp")
}

func TestKeyNeverPutsTheValueInAnError(t *testing.T) {
	t.Setenv(keyName(), "")
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte(keyName()+"=\n"), 0o600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	_, err := Key(path)
	if err == nil {
		t.Fatal("expected an empty value to be refused")
	}
	if strings.Contains(err.Error(), "=") {
		t.Fatalf("the error looks like it carries an assignment: %v", err)
	}
}
