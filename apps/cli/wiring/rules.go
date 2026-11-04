package wiring

import (
	"cmp"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"

	"tofu/internal/agent/loop"
	"tofu/internal/host"
	"tofu/internal/judge/gate"
	"tofu/internal/judge/points"
	rule "tofu/internal/librarian/rules"
	"tofu/internal/project/boardy"
	settingspkg "tofu/internal/store/settings"
	"tofu/internal/sys"
	"tofu/internal/tools/shell/bash/sift"
	shipped "tofu/library"
)

const (
	LibraryRoot         = "library"
	RulesFromTheBinary  = "the binary"
	RulesFromTheProject = "the project"
	PointKind           = "point"
	ruleOn              = "on"
)

const (
	boardLeadRule     = "board_lead"
	boardSubAgentRule = "board_sub_agent"
	boardRulesRule    = "board_rules"
	keepsSlotsRule    = "keeps_slots"
)

type LayerOverride struct {
	Layer string
	rule.Overriding
}

type RuleStack struct {
	Rules      []rule.Rule
	ShippedOff []rule.Rule
	BySetting  []string
	Origin     string
	Under      map[string][]rule.Rule
	Overrides  []LayerOverride
}

type RuleLayer struct{ Name, Dir, Flag string }

func RuleSetting(id string) string {
	switch id {
	case VerifySubAgentsRule:
		return settingspkg.VerifySubAgents
	case boardLeadRule, boardSubAgentRule, boardRulesRule, keepsSlotsRule:
		return boardy.ManagementSetting
	}
	return ""
}

func ruleSettingOnValue(key string) string {
	if key == boardy.ManagementSetting {
		return string(boardy.ManagementBoardy)
	}
	return "true"
}

func (o LayerOverride) Listing() *host.OverrideListing {
	r := o.Rule
	listed := &host.OverrideListing{RuleID: cmp.Or(r.Override.Of, r.ID), Version: r.Override.Version, Current: o.Base.Version, Layer: o.Layer, Change: "text", Text: r.Text,
		Reason: r.Override.Reason, By: string(r.Override.By), At: r.Override.At, Stale: o.Stale, File: r.File}
	switch r.Mode {
	case rule.ModeOff:
		listed.Change, listed.Text = string(rule.ModeOff), ""
	case rule.ModeShadow:
		listed.Change, listed.Text = ruleOn, ""
	}
	return listed
}

func StackRules(library, project string) (RuleStack, error) {
	rules, origin, err := BaseRules(library)
	if err != nil {
		return RuleStack{}, err
	}
	layers, err := UserRuleLayers(project)
	if err != nil {
		return RuleStack{}, err
	}
	stack := RuleStack{Origin: origin, Under: map[string][]rule.Rule{}}
	for _, layer := range layers {
		over, err := layer.Load()
		if err != nil {
			return RuleStack{}, err
		}
		stack.Under[layer.Name] = rules
		for _, found := range rule.Overrides(rules, over) {
			stack.Overrides = append(stack.Overrides, LayerOverride{layer.Name, found})
		}
		rules = rule.Layer(rules, over)
	}
	for i, one := range rules {
		key := RuleSetting(one.ID)
		if key == "" {
			continue
		}
		on := settingspkg.IntSaying(project, key, nil) != 0
		if key == boardy.ManagementSetting {
			on = BoardManaged(project)
		}
		mode := rule.ModeOff
		if on {
			mode = rule.ModeShadow
		}
		if (one.Mode == rule.ModeOff) != (mode == rule.ModeOff) {
			rules[i].Mode, stack.BySetting = mode, append(stack.BySetting, one.ID)
		}
	}
	stack.Rules = slices.DeleteFunc(slices.Clone(rules), func(r rule.Rule) bool { return r.Mode == rule.ModeOff })
	stack.ShippedOff = slices.DeleteFunc(rules, func(r rule.Rule) bool { return r.Mode != rule.ModeOff })
	return stack, nil
}

func (s RuleStack) SwitchOn(id, project string) string {
	key := RuleSetting(id)
	switch {
	case slices.Contains(s.BySetting, id):
		return "off by the setting " + key + ": tofu settings set " + key + " " + ruleSettingOnValue(key)
	case key == boardy.ManagementSetting && !ProjectHasBoard(project):
		return "off until the project has a board: tofu boardy init"
	case key != "":
		return "ships off: tofu rules restore " + id + ", or tofu settings set " + key + " " + ruleSettingOnValue(key)
	}
	return "ships off: tofu rules restore " + id
}

func ShadowCheck(r rule.Rule) string {
	if r.Checker == "" || r.Mode != rule.ModeShadow {
		return ""
	}
	return "its check reports and blocks nothing, and calibration is what enforces it"
}

func (s RuleStack) Applied(r rule.Rule) *host.OverrideListing {
	at := slices.IndexFunc(s.Overrides, func(o LayerOverride) bool { return o.Rule.File == r.File })
	if at < 0 {
		return nil
	}
	return s.Overrides[at].Listing()
}

func (s RuleStack) SwitchedOff() []LayerOverride {
	return slices.DeleteFunc(slices.Clone(s.Overrides), func(o LayerOverride) bool { return o.Stale || o.Rule.Mode != rule.ModeOff })
}

func LoadRules(library, project string) ([]rule.Rule, string, error) {
	stack, err := StackRules(library, project)
	return stack.Rules, stack.Origin, err
}

func BaseRules(override string) ([]rule.Rule, string, error) {
	if override != "" {
		rules, err := rule.LoadDir(override)
		return rules, override, err
	}
	libraryDir, err := sys.LibraryDir()
	if err != nil {
		return nil, "", err
	}
	isDir, err := sys.IsDir(libraryDir)
	if err != nil {
		return nil, "", err
	}
	rules, err := rule.LoadFS(shipped.Files(), LibraryRoot)
	if err != nil || !isDir {
		return rules, RulesFromTheBinary, err
	}
	copied, err := rule.LoadDir(libraryDir)
	if err != nil {
		return nil, "", err
	}
	rules = slices.DeleteFunc(rules, func(r rule.Rule) bool {
		return slices.ContainsFunc(copied, func(c rule.Rule) bool { return c.ID == r.ID })
	})
	return append(rules, copied...), RulesFromTheProject, nil
}

func PointListing(point host.DoctorRule) host.RuleListing {
	if point.Point == ShellSiftPoint {
		point = ShellSiftRule(point)
	}
	listed := host.RuleListing{ID: point.Point, Kind: PointKind, Origin: "shipped", Mode: point.Mode, Switch: point.Fallback}
	if point.Origin == string(gate.OriginProject) {
		listed.Origin = "library"
	}
	if point.Unusable != "" {
		listed.Switch = "unusable: " + point.Unusable
	}
	return listed
}

func ShellSiftRule(point host.DoctorRule) host.DoctorRule {
	point.Fallback, point.Unusable, point.ThresholdsFrom = "", "", "the rule"
	sifter, _, err := BuildShellSift(SiftFollowsTheTable, "")
	if err == nil {
		if chosen, _ := sifter.Methods.Of(sift.ShellSchema); chosen.Method == points.Unwired {
			err = points.UnwiredError{Choice: chosen, File: sifter.Methods.File}
		}
	}
	var contradiction loop.RuleContradictsTheTableError
	var unwired points.UnwiredError
	switch {
	case errors.As(err, &contradiction) || errors.As(err, &unwired):
		point.Mode, point.Fallback = "off", "no shell result is cut: "+err.Error()
	case err != nil:
		point.Mode, point.Unusable = "", err.Error()
	default:
		point.Mode = gate.ModeEnforced.String()
	}
	return point
}

func OverrideWhy(o host.OverrideListing) string {
	return "overridden in " + o.Layer + ": " + cmp.Or(o.Reason, "no reason given")
}

func OverrideRef(o host.OverrideListing) string {
	if o.Version == 0 {
		return o.RuleID
	}
	return o.RuleID + "@" + strconv.Itoa(o.Version)
}

func OverrideStaleWhy(o host.OverrideListing) string {
	if o.Current == 0 {
		return "stale: no rule " + o.RuleID + " runs below it"
	}
	return "stale: the rule is at @" + strconv.Itoa(o.Current) + " and runs unchanged"
}

func UserRuleLayers(project string) ([]RuleLayer, error) {
	home, err := sys.HomeConfigDir()
	if err != nil {
		return nil, err
	}
	full, err := filepath.Abs(project)
	if err != nil {
		return nil, err
	}
	projectFlag := ""
	if project != "." {
		projectFlag = " --dir " + full
	}
	return []RuleLayer{{"global", filepath.Join(home, "rules"), " --global"}, {"project", filepath.Join(sys.StateDir(full), "rules"), projectFlag}}, nil
}

func (l RuleLayer) Load() ([]rule.Rule, error) {
	isDir, err := sys.IsDir(l.Dir)
	if err != nil || !isDir {
		return nil, err
	}
	return rule.LoadDir(l.Dir)
}

func (l RuleLayer) Find(id string) (rule.Rule, bool, error) {
	loaded, err := l.Load()
	at := slices.IndexFunc(loaded, func(r rule.Rule) bool { return r.ID == id || r.Override.Of == id })
	if err != nil || at < 0 {
		return rule.Rule{}, false, err
	}
	return loaded[at], true, nil
}

func (l RuleLayer) Again(r rule.Rule) string {
	switch {
	case r.Override.Of != "" && r.Mode == rule.ModeOff:
		return fmt.Sprintf("tofu rules off%s %s --reason %q", l.Flag, r.Override.Of, r.Override.Reason)
	case r.Override.Of != "" && r.Mode == rule.ModeShadow:
		return "tofu rules restore" + l.Flag + " " + r.Override.Of
	case r.Override.Of != "":
		return fmt.Sprintf("tofu rules add%s %s %q --reason %q", l.Flag, r.ID, r.Text, r.Override.Reason)
	case r.Mode == rule.ModeOff:
		return "tofu rules off" + l.Flag + " " + r.ID + ` --reason "<why>"`
	}
	return fmt.Sprintf("tofu rules add%s --concern %s %s %q", l.Flag, r.Concern, r.ID, r.Text)
}
