package state

import (
	"crypto/sha256"
	"encoding/hex"
	"path"
	"sort"
	"strings"
)

const fingerprintDigestChars = 16

const strippedValue = "_"

func FingerprintOf(in ToolGateInput) string {
	tool := strings.ToLower(strings.TrimSpace(in.Tool))
	if tool == "" {
		return ""
	}
	repository := path.Base(slashedLower(in.ProjectDir))
	normalised := strings.Join([]string{
		tool,
		argumentShape(in.Input),
		repository,
		workingDirWithinRepository(in.Cwd, in.ProjectDir),
	}, "\n")
	sum := sha256.Sum256([]byte(normalised))
	return tool + "." + hex.EncodeToString(sum[:])[:fingerprintDigestChars]
}

func argumentShape(input map[string]any) string {
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	shaped := make([]string, 0, len(keys))
	for _, key := range keys {
		shaped = append(shaped, key+"="+valueShape(key, input[key]))
	}
	return strings.Join(shaped, " ")
}

func valueShape(key string, value any) string {
	switch typed := value.(type) {
	case string:
		if key == "command" {
			return "sh(" + commandShape(typed) + ")"
		}
		return strippedValue
	case map[string]any:
		return "{" + argumentShape(typed) + "}"
	case []any:
		return "[]"
	}
	return strippedValue
}

func commandShape(command string) string {
	shell, _ := splitHeredocs(command)
	shaped := []string{}
	for _, segment := range segments(tokens(shell)) {
		shaped = append(shaped, segmentShape(segment))
	}
	return strings.Join(shaped, " ; ")
}

func segmentShape(segment []string) string {
	shaped := make([]string, 0, len(segment))
	operandSeen := false
	for i, token := range segment {
		switch {
		case i == 0:
			shaped = append(shaped, programName(token))
		case strings.HasPrefix(token, "-"):
			flag, _, carriesValue := strings.Cut(token, "=")
			if carriesValue {
				flag += "=" + strippedValue
			}
			shaped = append(shaped, flag)
		case !operandSeen && readsAsSubcommand(token):
			operandSeen = true
			shaped = append(shaped, token)
		default:
			operandSeen = true
			shaped = append(shaped, strippedValue)
		}
	}
	return strings.Join(shaped, " ")
}

func readsAsSubcommand(token string) bool {
	if len(token) < 2 {
		return false
	}
	for _, c := range token {
		if (c < 'a' || c > 'z') && c != '-' {
			return false
		}
	}
	return true
}

func programName(token string) string {
	name := token
	if cut := strings.LastIndexAny(name, "/\\"); cut >= 0 {
		name = name[cut+1:]
	}
	return strings.TrimSuffix(strings.ToLower(name), ".exe")
}

func slashedLower(raw string) string {
	if raw == "" {
		return ""
	}
	return strings.ToLower(path.Clean(strings.ReplaceAll(raw, "\\", "/")))
}

func workingDirWithinRepository(cwd, projectDir string) string {
	here := slashedLower(cwd)
	root := strings.TrimSuffix(slashedLower(projectDir), "/")
	switch {
	case root == "":
		return here
	case here == root:
		return "."
	case strings.HasPrefix(here, root+"/"):
		return here[len(root)+1:]
	}
	return here
}
