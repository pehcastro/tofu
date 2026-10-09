package subagent

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const (
	notRead     = "bash runs only"
	sourceShell = "is source: change it with edit or write"
)

type bashRow struct {
	cmd, refusal string
}

func drive(t *testing.T, boundary *Boundary, grammar Grammar, rows []bashRow) {
	t.Helper()
	for _, row := range rows {
		err := boundary.Bash(row.cmd, grammar)
		t.Logf("%v %q -> %v", boundary.Owns(), row.cmd, err)
		if !refusedAsWanted(err, row.refusal) {
			t.Errorf("owns %v, %q: want %q, got %v", boundary.Owns(), row.cmd, row.refusal, err)
		}
	}
}

func project(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	for _, made := range []string{"src", "p"} {
		if err := os.MkdirAll(made, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.ToSlash(dir)
}

func TestWithNoOwnsBashRefusesEveryCommandThatWroteInRoundOne(t *testing.T) {
	root := project(t)
	temp := filepath.ToSlash(os.TempDir())
	drive(t, NewBoundary("sub-1", root+"/.tofu/scratch/a1", nil), POSIX, []bashRow{
		{"rm README.md", notOwned},
		{"rm -rf .git", notOwned},
		{"git commit --allow-empty -q -m probe", notRead},
		{"git checkout -- README.md", notRead},
		{"git reset -q --hard HEAD", notRead},
		{"git apply /tmp/p.patch", notRead},
		{"git -c core.pager=less log", notRead},
		{"git --git-dir=/tmp/r/.git log", notRead},
		{"git diff --output=d.txt", notRead},
		{"git grep -Ovim x", notRead},
		{"git -C /tmp/r status", notRead},
		{"git branch gone", notRead},
		{`powershell -NoProfile -Command "Set-Content -Path ps-set.txt -Value x"`, notRead},
		{`cmd //c "echo x> cmd.txt"`, notRead},
		{`python -c "open('py.txt','w').write('x')"`, notRead},
		{"python - <<'EOF'\nopen('hdpy.txt','w').write('x')\nEOF", notRead},
		{"python -m http.server", notRead},
		{`bash -c 'echo x > bashc.txt'`, notRead},
		{`sh -c 'echo x > shc.txt'`, notRead},
		{`eval 'echo x > eval.txt'`, notRead},
		{`awk '{print > "awk.txt"}' README.md`, notRead},
		{`perl -e 'open(F, ">perl.txt")'`, notRead},
		{`node -e "require('fs').writeFileSync('n.txt','x')"`, notRead},
		{"touch touched.txt", notOwned},
		{"mkdir made", notOwned},
		{"dd if=README.md of=dd.txt", notRead},
		{"truncate -s 0 README.md", notRead},
		{"install -m 644 README.md inst.txt", notRead},
		{"tar -xf /tmp/a.tar", notRead},
		{"curl -s -o curl.txt file:///etc/hosts", notRead},
		{"go mod init example.com/probe", notRead},
		{"ln -s README.md link.md", notRead},
		{"find . -name main.go -delete", notRead},
		{"find . -name '*.go' -exec rm {} ;", notRead},
		{"find . -fprint found.txt", notRead},
		{"sed -n 'w sedw.txt' README.md", notRead},
		{"sed 's/alpha/beta/w sw.txt' README.md", notRead},
		{"sed 's/a;b/c/w sw.txt' README.md", notRead},
		{"sed -e '1e touch x' README.md", notRead},
		{"sed '/x/{p;W out.txt\n}' README.md", notRead},
		{"sed -f script.sed README.md", notRead},
		{"sed -ne 1p -ne 'w out.txt' README.md", notRead},
		{"sed -n -es/a/b/wout.txt README.md", notRead},
		{"sed -l 5 'w out.txt' README.md", notRead},
		{"sed -nf script.sed README.md", notRead},
		{"sed -i s/alpha/beta/ README.md", notOwned},
		{"sort -o sorted.txt README.md", notRead},
		{"sort -uo sorted.txt README.md", notRead},
		{"sort --compress-program=x README.md", notRead},
		{"uniq README.md out.txt", notRead},
		{"rg --pre ./x.sh alpha", notRead},
		{"cp README.md copy.txt", notOwned},
		{"cp -s README.md /tmp/l", notRead},
		{"cp -rl src /tmp/l", notRead},
		{"mv README.md moved.txt", notOwned},
		{"mv README.md /tmp/gone.md", notOwned},
		{"mv main.go " + temp + "/gone.go", notOwned},
		{"printf 'echo pwn > pwn.txt\\n' > /tmp/s.sh && bash /tmp/s.sh", notRead},
		{"echo x > NUL", notOwned},
		{"echo x > out.txt", notOwned},
		{"echo x > " + root + "/abs.txt", notOwned},
		{"exec 3<> fd.txt", notRead},
		{"cat README.md 1<>rw.txt", notOwned},
		{"PATH=/tmp:$PATH ls", notRead},
		{"LD_PRELOAD=/tmp/x.so ls", notRead},
		{"GOFLAGS=-toolexec=/tmp/x go test ./p", notRead},
		{"H=x; ls", notRead},
		{"ls $(rm README.md)", notRead},
		{"ls `rm README.md`", notRead},
		{`echo "$(touch x)"`, notRead},
		{"ls ${X@P}", notRead},
		{"ls ${(e)X}", notRead},
		{"echo $((X))", notRead},
		{"/tmp/ls", notRead},
		{"./ls", notRead},
		{"$X README.md", notRead},
		{"{ ls; }", notRead},
		{"for f in a; do cat $f; done", notRead},
		{"xargs rm < list.txt", notRead},
		{"env ls", notRead},
		{"go test -exec /tmp/x ./p", notRead},
		{"go test -toolexec=x ./p", notRead},
		{"go test ./p -update", notRead},
		{"go test -c ./p", notRead},
		{"go test -coverprofile=c.out ./p", notRead},
		{"go test -mod=mod ./p", notRead},
		{"go test -mod mod ./p", notRead},
		{"go build ./p", notRead},
		{"go build -o /dev/null ./p", notRead},
		{"go run ./p", notRead},
		{"go env -w GOFLAGS=-x", notRead},
		{"go generate ./p", notRead},
		{"go test /tmp/x_test.go", notRead},
		{"go test ./.tofu/scratch/a1", notRead},
		{"go test ../other/...", notRead},
		{"cd /tmp && go test ./p", notRead},
		{"cd .. && cargo test", notRead},
		{"cd && go vet ./p", notRead},
		{"go test ./...", treeWide},
		{"cargo fmt", notRead},
		{"cargo clippy --fix", notRead},
		{"cargo install ripgrep", notRead},
		{"cargo test --manifest-path /tmp/c/Cargo.toml", notRead},
		{"npm install", notRead},
		{"npm run deploy", notRead},
		{"npm test -- -u", notRead},
		{"npx cowsay hi", notRead},
		{"make clean", notRead},
		{"make", notRead},
		{"make -f /tmp/m.mk test", notRead},
		{"make test SHELL=/tmp/x", notRead},
		{"tsc", notRead},
		{"npx eslint --fix src", notRead},
		{"npx prettier --write src", notRead},
		{"npx vitest -u", notRead},
		{"ruff format src", notRead},
		{"ruff check --fix src", notRead},
		{"pytest --junitxml=r.xml", notRead},
		{"uv run --with x pytest", notRead},
		{"uv run python -c 'print(1)'", notRead},
		{"rtk proxy python -c 'print(1)'", notRead},
		{"rtk", notRead},
	})
}

func TestWithNoOwnsBashStillRunsReadsAndChecks(t *testing.T) {
	root := project(t)
	scratch := root + "/.tofu/scratch/a1"
	drive(t, NewBoundary("sub-1", scratch, nil), POSIX, []bashRow{
		{"ls", ""},
		{"ls -la src 2>/dev/null", ""},
		{"cat README.md", ""},
		{"head -n 5 README.md && tail -n 2 README.md", ""},
		{"wc -l README.md", ""},
		{"grep -rn alpha .", ""},
		{"rg -n 'a{2,3}' src", ""},
		{"git status", ""},
		{"git --no-pager log --oneline -3", ""},
		{"git diff HEAD", ""},
		{"git show HEAD:README.md", ""},
		{"git blame README.md", ""},
		{"git status -u", ""},
		{"git -C src log -1", ""},
		{"git branch --show-current", ""},
		{"git ls-files | wc -l", ""},
		{"sed -n 1,20p README.md", ""},
		{"sed -n '$p' README.md", ""},
		{"sed 's/alpha/beta/g' README.md", ""},
		{"sed -e 's/a/b/' -e '/x/d' README.md", ""},
		{"sed -ne 1p README.md", ""},
		{"sed -n -e1,5p README.md", ""},
		{"find . -name '*.go' -type f", ""},
		{"echo x > /dev/null", ""},
		{"cat README.md | sort | uniq -c", ""},
		{"cd src && ls", ""},
		{"diff README.md main.go", ""},
		{"jq . package.json", ""},
		{"test -f README.md && echo yes", ""},
		{"sleep 1", ""},
		{"echo x > /tmp/x.txt", ""},
		{"mkdir -p " + scratch + "/shots && rm " + scratch + "/run.log", ""},
		{"go vet ./p", ""},
		{"go test ./p/...", ""},
		{"go test -run 'TestX$' -count=1 -v ./p", ""},
		{"go list -m", ""},
		{"CI=1 go test ./p", ""},
		{"rtk go test ./p/", ""},
		{"rtk proxy go vet ./p", ""},
		{"cargo test -p parser", ""},
		{"cargo clippy --all-targets -- -D warnings", ""},
		{"cd crates/parser && cargo test status", ""},
		{"cargo fmt --check", ""},
		{"npm test", ""},
		{"pnpm test", ""},
		{"rtk npm run typecheck", ""},
		{"bun test", ""},
		{"npx vue-tsc --noEmit", ""},
		{"make vet && make test", ""},
		{"make testall", ""},
		{"pytest -q", ""},
		{"uv run --frozen python -m pytest -q", ""},
		{"uv run ruff check src && uv run pytest tests/test_x.py::test_y", ""},
		{"CI=1 ruff check . ; uv run --frozen python -m pytest -q", ""},
		{"ruff format --check src", ""},
	})
}

func TestBashHoldsWritesInsidePartOfTheTreeToTodaysRules(t *testing.T) {
	project(t)
	drive(t, NewBoundary("sub-1", "", []string{"src/**"}), POSIX, []bashRow{
		{"echo x > src/a.txt", ""},
		{"cp README.md src/a.txt", ""},
		{"echo x | tee src/b.txt", ""},
		{"mkdir -p src/made && touch src/made/a.txt", ""},
		{"rm src/a.txt", ""},
		{"mv src/a.txt src/b.txt", ""},
		{"sed -i s/a/b/ src/a.txt", ""},
		{"cd src && echo x > a.txt", ""},
		{"echo x > src/a.go", sourceShell},
		{"echo x > main.go", notOwned},
		{"mv src/a.txt a.txt", notOwned},
		{"mv a.txt src/a.txt", notOwned},
		{`python -c "open('src/a.txt','w').write('x')"`, notRead},
		{"cargo fmt", notRead},
		{"gofmt -w src/x.go", notRead},
	})
	drive(t, NewBoundary("files", "", []string{"**"}), POSIX, []bashRow{
		{`python -c "open('a.txt','w').write('x')"`, ""},
		{"rm README.md", ""},
		{"go test ./...", treeWide},
	})
}

func TestPowerShellIsReadForWhatPowerShellRuns(t *testing.T) {
	project(t)
	rows := []bashRow{
		{"'x' > out.txt", notRead},
		{"echo x > out.txt", notOwned},
		{"Set-Content -Path sc.txt -Value x", notRead},
		{"Add-Content -Path README.md -Value x", notRead},
		{"'x' | Out-File of.txt", notRead},
		{"echo x | tee tee.txt", notOwned},
		{"New-Item -ItemType File ni.txt", notRead},
		{"Remove-Item README.md", notRead},
		{"rm README.md", notOwned},
		{"cp -Destination cp.txt README.md", notOwned},
		{"Copy-Item README.md ci.txt", notRead},
		{"Move-Item README.md mi.txt", notRead},
		{`[IO.File]::WriteAllText("$PWD\net.txt", "x")`, notRead},
		{"$f = 'v.txt'; 'x' > $f", notRead},
		{`cmd /c "echo x> cmd.txt"`, notRead},
		{"git commit --allow-empty -q -m probe", notRead},
		{"Start-Job { Set-Content -Path job.txt -Value x } | Wait-Job", notRead},
		{"Get-ChildItem | ForEach-Object { Remove-Item $_ }", notRead},
		{"Get-ChildItem | Get-Item -Path { Remove-Item README.md; 'a' }", notRead},
		{"Get-ChildItem <# x\n#> ; Remove-Item README.md", notRead},
		{`Write-Output "a\" ; Remove-Item README.md ; \""`, notRead},
		{"Get-Content README.md `\n; Remove-Item x", notRead},
		{"& ('Remove' + '-Item') README.md", notRead},
		{"iex 'Remove-Item README.md'", notRead},
		{"Get-ChildItem", ""},
		{"Get-Content README.md | Measure-Object -Line", ""},
		{"Select-String -Pattern alpha -Path *.md", ""},
		{"ls src; cat README.md", ""},
		{"git status", ""},
		{"echo x > $null", ""},
	}
	drive(t, NewBoundary("sub-1", "", nil), PowerShell, rows)
	drive(t, NewBoundary("sub-1", "", []string{"src/**"}), PowerShell, []bashRow{
		{`echo x > src\a.txt`, ""},
		{`echo x > sr\c/a.txt`, notOwned},
	})
}

func TestADeviceNameIsADeviceOnlyWhereTheShellMakesItOne(t *testing.T) {
	project(t)
	boundary := NewBoundary("sub-1", "", nil)
	windowsDevice := notOwned
	if runtime.GOOS == "windows" {
		windowsDevice = ""
	}
	drive(t, boundary, POSIX, []bashRow{{"echo x > NUL", notOwned}, {"echo x > nul", notOwned}, {"echo x > /dev/null", ""}})
	drive(t, boundary, PowerShell, []bashRow{{"echo x > NUL", windowsDevice}, {"echo x > $null", ""}})
}

func TestTheTempFolderIsExemptOnlyWhenNoLinkLeadsOutOfIt(t *testing.T) {
	root := project(t)
	temp := filepath.ToSlash(os.TempDir())
	link := filepath.Join(os.TempDir(), "tofu1234-link-"+filepath.Base(root))
	if err := os.Symlink(root, link); err != nil {
		t.Skipf("this host cannot make a symlink without privilege: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(link) })
	drive(t, NewBoundary("sub-1", "", nil), POSIX, []bashRow{
		{"echo x > " + temp + "/tofu1234-plain.txt", ""},
		{"echo x > " + filepath.ToSlash(link) + "/through.txt", notOwned},
		{"cd " + filepath.ToSlash(link) + " && echo x > through.txt", notOwned},
	})
}

func TestAProjectInsideTheTempFolderIsNotExemptAsTemp(t *testing.T) {
	root := project(t)
	if !strings.HasPrefix(strings.ToLower(root), strings.ToLower(filepath.ToSlash(os.TempDir()))) {
		t.Skipf("t.TempDir %s is not under os.TempDir %s", root, os.TempDir())
	}
	drive(t, NewBoundary("sub-1", "", nil), POSIX, []bashRow{{"echo x > " + root + "/abs.txt", notOwned}})
}

func TestCaseFoldsOnlyWhereTheFileSystemDoes(t *testing.T) {
	project(t)
	upper := notOwned
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		upper = ""
	}
	drive(t, NewBoundary("sub-1", "", []string{"src/**"}), POSIX, []bashRow{{"echo x > Src/a.txt", upper}, {"echo x > src/a.txt", ""}})
}

func TestGrammarFollowsTheShellThatRuns(t *testing.T) {
	for shell, want := range map[string]Grammar{
		`C:\Program Files\Git\bin\bash.exe`: POSIX,
		"/bin/zsh":                          POSIX,
		`C:\WINDOWS\System32\WindowsPowerShell\v1.0\powershell.exe`: PowerShell,
		"/usr/local/bin/pwsh": PowerShell,
	} {
		if got := GrammarOf(shell); got != want {
			t.Errorf("%s: grammar %v, want %v", shell, got, want)
		}
	}
}
