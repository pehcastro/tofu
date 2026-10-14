package bluemonday

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/microcosm-cc/bluemonday/css"
)

type Policy struct {
	initialized bool

	addSpaces bool

	requireNoFollow bool

	requireNoFollowFullyQualifiedLinks bool

	requireNoReferrer bool

	requireNoReferrerFullyQualifiedLinks bool

	requireCrossOriginAnonymous bool

	requireSandboxOnIFrame map[string]bool

	addTargetBlankToFullyQualifiedLinks bool

	requireParseableURLs bool

	allowRelativeURLs bool

	allowDataAttributes bool

	allowComments bool

	elsAndAttrs map[string]map[string][]attrPolicy

	elsMatchingAndAttrs map[*regexp.Regexp]map[string][]attrPolicy

	globalAttrs map[string][]attrPolicy

	elsAndStyles map[string]map[string][]stylePolicy

	elsMatchingAndStyles map[*regexp.Regexp]map[string][]stylePolicy

	globalStyles map[string][]stylePolicy

	allowURLSchemes map[string][]urlPolicy

	allowURLSchemeRegexps []*regexp.Regexp

	srcRewriter urlRewriter

	setOfElementsAllowedWithoutAttrs map[string]struct{}

	setOfElementsMatchingAllowedWithoutAttrs []*regexp.Regexp

	setOfElementsToSkipContent map[string]struct{}

	allowUnsafe bool
}

type attrPolicy struct {
	regexp *regexp.Regexp
}

type stylePolicy struct {
	handler func(string) bool

	regexp *regexp.Regexp

	enum []string
}

type attrPolicyBuilder struct {
	p *Policy

	attrNames  []string
	regexp     *regexp.Regexp
	allowEmpty bool
}

type stylePolicyBuilder struct {
	p *Policy

	propertyNames []string
	regexp        *regexp.Regexp
	enum          []string
	handler       func(string) bool
}

type urlPolicy func(url *url.URL) (allowUrl bool)

type urlRewriter func(*url.URL)

type SandboxValue int64

const (
	SandboxAllowDownloads SandboxValue = iota
	SandboxAllowDownloadsWithoutUserActivation
	SandboxAllowForms
	SandboxAllowModals
	SandboxAllowOrientationLock
	SandboxAllowPointerLock
	SandboxAllowPopups
	SandboxAllowPopupsToEscapeSandbox
	SandboxAllowPresentation
	SandboxAllowSameOrigin
	SandboxAllowScripts
	SandboxAllowStorageAccessByUserActivation
	SandboxAllowTopNavigation
	SandboxAllowTopNavigationByUserActivation
)

func (p *Policy) init() {
	if !p.initialized {
		p.elsAndAttrs = make(map[string]map[string][]attrPolicy)
		p.elsMatchingAndAttrs = make(map[*regexp.Regexp]map[string][]attrPolicy)
		p.globalAttrs = make(map[string][]attrPolicy)
		p.elsAndStyles = make(map[string]map[string][]stylePolicy)
		p.elsMatchingAndStyles = make(map[*regexp.Regexp]map[string][]stylePolicy)
		p.globalStyles = make(map[string][]stylePolicy)
		p.allowURLSchemes = make(map[string][]urlPolicy)
		p.allowURLSchemeRegexps = make([]*regexp.Regexp, 0)
		p.setOfElementsAllowedWithoutAttrs = make(map[string]struct{})
		p.setOfElementsToSkipContent = make(map[string]struct{})
		p.initialized = true
	}
}

func NewPolicy() *Policy {

	p := Policy{}

	p.addDefaultElementsWithoutAttrs()
	p.addDefaultSkipElementContent()

	return &p
}

func (p *Policy) AllowAttrs(attrNames ...string) *attrPolicyBuilder {

	p.init()

	abp := attrPolicyBuilder{
		p:          p,
		allowEmpty: false,
	}

	for _, attrName := range attrNames {
		abp.attrNames = append(abp.attrNames, strings.ToLower(attrName))
	}

	return &abp
}

func (p *Policy) AllowDataAttributes() {
	p.allowDataAttributes = true
}

func (p *Policy) AllowComments() {
	p.allowComments = true
}

func (p *Policy) AllowNoAttrs() *attrPolicyBuilder {

	p.init()

	abp := attrPolicyBuilder{
		p:          p,
		allowEmpty: true,
	}
	return &abp
}

func (abp *attrPolicyBuilder) AllowNoAttrs() *attrPolicyBuilder {

	abp.allowEmpty = true

	return abp
}

func (abp *attrPolicyBuilder) Matching(regex *regexp.Regexp) *attrPolicyBuilder {

	abp.regexp = regex

	return abp
}

func (abp *attrPolicyBuilder) OnElements(elements ...string) *Policy {

	for _, element := range elements {
		element = strings.ToLower(element)

		for _, attr := range abp.attrNames {

			if _, ok := abp.p.elsAndAttrs[element]; !ok {
				abp.p.elsAndAttrs[element] = make(map[string][]attrPolicy)
			}

			ap := attrPolicy{}
			if abp.regexp != nil {
				ap.regexp = abp.regexp
			}

			abp.p.elsAndAttrs[element][attr] = append(abp.p.elsAndAttrs[element][attr], ap)
		}

		if abp.allowEmpty {
			abp.p.setOfElementsAllowedWithoutAttrs[element] = struct{}{}

			if _, ok := abp.p.elsAndAttrs[element]; !ok {
				abp.p.elsAndAttrs[element] = make(map[string][]attrPolicy)
			}
		}
	}

	return abp.p
}

func (abp *attrPolicyBuilder) OnElementsMatching(regex *regexp.Regexp) *Policy {
	for _, attr := range abp.attrNames {
		if _, ok := abp.p.elsMatchingAndAttrs[regex]; !ok {
			abp.p.elsMatchingAndAttrs[regex] = make(map[string][]attrPolicy)
		}
		ap := attrPolicy{}
		if abp.regexp != nil {
			ap.regexp = abp.regexp
		}
		abp.p.elsMatchingAndAttrs[regex][attr] = append(abp.p.elsMatchingAndAttrs[regex][attr], ap)
	}

	if abp.allowEmpty {
		abp.p.setOfElementsMatchingAllowedWithoutAttrs = append(abp.p.setOfElementsMatchingAllowedWithoutAttrs, regex)
		if _, ok := abp.p.elsMatchingAndAttrs[regex]; !ok {
			abp.p.elsMatchingAndAttrs[regex] = make(map[string][]attrPolicy)
		}
	}

	return abp.p
}

func (abp *attrPolicyBuilder) Globally() *Policy {

	for _, attr := range abp.attrNames {
		if _, ok := abp.p.globalAttrs[attr]; !ok {
			abp.p.globalAttrs[attr] = []attrPolicy{}
		}

		ap := attrPolicy{}
		if abp.regexp != nil {
			ap.regexp = abp.regexp
		}

		abp.p.globalAttrs[attr] = append(abp.p.globalAttrs[attr], ap)
	}

	return abp.p
}

func (p *Policy) AllowStyles(propertyNames ...string) *stylePolicyBuilder {

	p.init()

	abp := stylePolicyBuilder{
		p: p,
	}

	for _, propertyName := range propertyNames {
		abp.propertyNames = append(abp.propertyNames, strings.ToLower(propertyName))
	}

	return &abp
}

func (spb *stylePolicyBuilder) Matching(regex *regexp.Regexp) *stylePolicyBuilder {

	spb.regexp = regex

	return spb
}

func (spb *stylePolicyBuilder) MatchingEnum(enum ...string) *stylePolicyBuilder {

	spb.enum = enum

	return spb
}

func (spb *stylePolicyBuilder) MatchingHandler(handler func(string) bool) *stylePolicyBuilder {

	spb.handler = handler

	return spb
}

func (spb *stylePolicyBuilder) OnElements(elements ...string) *Policy {

	for _, element := range elements {
		element = strings.ToLower(element)

		for _, attr := range spb.propertyNames {

			if _, ok := spb.p.elsAndStyles[element]; !ok {
				spb.p.elsAndStyles[element] = make(map[string][]stylePolicy)
			}

			sp := stylePolicy{}
			if spb.handler != nil {
				sp.handler = spb.handler
			} else if len(spb.enum) > 0 {
				sp.enum = spb.enum
			} else if spb.regexp != nil {
				sp.regexp = spb.regexp
			} else {
				sp.handler = css.GetDefaultHandler(attr)
			}
			spb.p.elsAndStyles[element][attr] = append(spb.p.elsAndStyles[element][attr], sp)
		}
	}

	return spb.p
}

func (spb *stylePolicyBuilder) OnElementsMatching(regex *regexp.Regexp) *Policy {

	for _, attr := range spb.propertyNames {

		if _, ok := spb.p.elsMatchingAndStyles[regex]; !ok {
			spb.p.elsMatchingAndStyles[regex] = make(map[string][]stylePolicy)
		}

		sp := stylePolicy{}
		if spb.handler != nil {
			sp.handler = spb.handler
		} else if len(spb.enum) > 0 {
			sp.enum = spb.enum
		} else if spb.regexp != nil {
			sp.regexp = spb.regexp
		} else {
			sp.handler = css.GetDefaultHandler(attr)
		}
		spb.p.elsMatchingAndStyles[regex][attr] = append(spb.p.elsMatchingAndStyles[regex][attr], sp)
	}

	return spb.p
}

func (spb *stylePolicyBuilder) Globally() *Policy {

	for _, attr := range spb.propertyNames {
		if _, ok := spb.p.globalStyles[attr]; !ok {
			spb.p.globalStyles[attr] = []stylePolicy{}
		}

		sp := stylePolicy{}
		if spb.handler != nil {
			sp.handler = spb.handler
		} else if len(spb.enum) > 0 {
			sp.enum = spb.enum
		} else if spb.regexp != nil {
			sp.regexp = spb.regexp
		} else {
			sp.handler = css.GetDefaultHandler(attr)
		}
		spb.p.globalStyles[attr] = append(spb.p.globalStyles[attr], sp)
	}

	return spb.p
}

func (p *Policy) AllowElements(names ...string) *Policy {
	p.init()

	for _, element := range names {
		element = strings.ToLower(element)

		if _, ok := p.elsAndAttrs[element]; !ok {
			p.elsAndAttrs[element] = make(map[string][]attrPolicy)
		}
	}

	return p
}

func (p *Policy) AllowElementsMatching(regex *regexp.Regexp) *Policy {
	p.init()
	if _, ok := p.elsMatchingAndAttrs[regex]; !ok {
		p.elsMatchingAndAttrs[regex] = make(map[string][]attrPolicy)
	}
	return p
}

func (p *Policy) AllowURLSchemesMatching(r *regexp.Regexp) *Policy {
	p.allowURLSchemeRegexps = append(p.allowURLSchemeRegexps, r)
	return p
}

func (p *Policy) RewriteSrc(fn urlRewriter) *Policy {
	p.srcRewriter = fn
	return p
}

func (p *Policy) RequireNoFollowOnLinks(require bool) *Policy {

	p.requireNoFollow = require
	p.requireParseableURLs = true

	return p
}

func (p *Policy) RequireNoFollowOnFullyQualifiedLinks(require bool) *Policy {

	p.requireNoFollowFullyQualifiedLinks = require
	p.requireParseableURLs = true

	return p
}

func (p *Policy) RequireNoReferrerOnLinks(require bool) *Policy {

	p.requireNoReferrer = require
	p.requireParseableURLs = true

	return p
}

func (p *Policy) RequireNoReferrerOnFullyQualifiedLinks(require bool) *Policy {

	p.requireNoReferrerFullyQualifiedLinks = require
	p.requireParseableURLs = true

	return p
}

func (p *Policy) RequireCrossOriginAnonymous(require bool) *Policy {

	p.requireCrossOriginAnonymous = require

	return p
}

func (p *Policy) AddTargetBlankToFullyQualifiedLinks(require bool) *Policy {

	p.addTargetBlankToFullyQualifiedLinks = require
	p.requireParseableURLs = true

	return p
}

func (p *Policy) RequireParseableURLs(require bool) *Policy {

	p.requireParseableURLs = require

	return p
}

func (p *Policy) AllowRelativeURLs(require bool) *Policy {

	p.RequireParseableURLs(true)
	p.allowRelativeURLs = require

	return p
}

func (p *Policy) AllowURLSchemes(schemes ...string) *Policy {
	p.init()

	p.RequireParseableURLs(true)

	for _, scheme := range schemes {
		scheme = strings.ToLower(scheme)

		p.allowURLSchemes[scheme] = nil
	}

	return p
}

func (p *Policy) AllowURLSchemeWithCustomPolicy(
	scheme string,
	urlPolicy func(url *url.URL) (allowUrl bool),
) *Policy {

	p.init()

	p.RequireParseableURLs(true)

	scheme = strings.ToLower(scheme)

	p.allowURLSchemes[scheme] = append(p.allowURLSchemes[scheme], urlPolicy)

	return p
}

func (p *Policy) RequireSandboxOnIFrame(vals ...SandboxValue) {
	p.requireSandboxOnIFrame = make(map[string]bool)

	for _, val := range vals {
		switch SandboxValue(val) {
		case SandboxAllowDownloads:
			p.requireSandboxOnIFrame["allow-downloads"] = true

		case SandboxAllowDownloadsWithoutUserActivation:
			p.requireSandboxOnIFrame["allow-downloads-without-user-activation"] = true

		case SandboxAllowForms:
			p.requireSandboxOnIFrame["allow-forms"] = true

		case SandboxAllowModals:
			p.requireSandboxOnIFrame["allow-modals"] = true

		case SandboxAllowOrientationLock:
			p.requireSandboxOnIFrame["allow-orientation-lock"] = true

		case SandboxAllowPointerLock:
			p.requireSandboxOnIFrame["allow-pointer-lock"] = true

		case SandboxAllowPopups:
			p.requireSandboxOnIFrame["allow-popups"] = true

		case SandboxAllowPopupsToEscapeSandbox:
			p.requireSandboxOnIFrame["allow-popups-to-escape-sandbox"] = true

		case SandboxAllowPresentation:
			p.requireSandboxOnIFrame["allow-presentation"] = true

		case SandboxAllowSameOrigin:
			p.requireSandboxOnIFrame["allow-same-origin"] = true

		case SandboxAllowScripts:
			p.requireSandboxOnIFrame["allow-scripts"] = true

		case SandboxAllowStorageAccessByUserActivation:
			p.requireSandboxOnIFrame["allow-storage-access-by-user-activation"] = true

		case SandboxAllowTopNavigation:
			p.requireSandboxOnIFrame["allow-top-navigation"] = true

		case SandboxAllowTopNavigationByUserActivation:
			p.requireSandboxOnIFrame["allow-top-navigation-by-user-activation"] = true
		}
	}
}

func (p *Policy) AddSpaceWhenStrippingTag(allow bool) *Policy {

	p.addSpaces = allow

	return p
}

func (p *Policy) SkipElementsContent(names ...string) *Policy {

	p.init()

	for _, element := range names {
		element = strings.ToLower(element)

		if _, ok := p.setOfElementsToSkipContent[element]; !ok {
			p.setOfElementsToSkipContent[element] = struct{}{}
		}
	}

	return p
}

func (p *Policy) AllowElementsContent(names ...string) *Policy {

	p.init()

	for _, element := range names {
		delete(p.setOfElementsToSkipContent, strings.ToLower(element))
	}

	return p
}

func (p *Policy) AllowUnsafe(allowUnsafe bool) *Policy {
	p.init()
	p.allowUnsafe = allowUnsafe
	return p
}

func (p *Policy) addDefaultElementsWithoutAttrs() {
	p.init()

	p.setOfElementsAllowedWithoutAttrs["abbr"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["acronym"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["address"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["article"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["aside"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["audio"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["b"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["bdi"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["blockquote"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["body"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["br"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["button"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["canvas"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["caption"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["center"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["cite"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["code"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["col"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["colgroup"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["datalist"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["dd"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["del"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["details"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["dfn"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["div"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["dl"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["dt"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["em"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["fieldset"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["figcaption"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["figure"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["footer"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["h1"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["h2"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["h3"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["h4"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["h5"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["h6"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["head"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["header"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["hgroup"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["hr"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["html"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["i"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["ins"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["kbd"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["li"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["mark"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["marquee"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["nav"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["ol"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["optgroup"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["option"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["p"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["picture"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["pre"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["q"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["rp"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["rt"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["ruby"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["s"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["samp"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["script"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["section"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["select"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["small"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["span"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["strike"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["strong"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["style"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["sub"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["summary"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["sup"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["svg"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["table"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["tbody"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["td"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["textarea"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["tfoot"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["th"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["thead"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["title"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["time"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["tr"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["tt"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["u"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["ul"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["var"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["video"] = struct{}{}
	p.setOfElementsAllowedWithoutAttrs["wbr"] = struct{}{}

}

func (p *Policy) addDefaultSkipElementContent() {
	p.init()

	p.setOfElementsToSkipContent["frame"] = struct{}{}
	p.setOfElementsToSkipContent["frameset"] = struct{}{}
	p.setOfElementsToSkipContent["iframe"] = struct{}{}
	p.setOfElementsToSkipContent["noembed"] = struct{}{}
	p.setOfElementsToSkipContent["noframes"] = struct{}{}
	p.setOfElementsToSkipContent["noscript"] = struct{}{}
	p.setOfElementsToSkipContent["nostyle"] = struct{}{}
	p.setOfElementsToSkipContent["object"] = struct{}{}
	p.setOfElementsToSkipContent["script"] = struct{}{}
	p.setOfElementsToSkipContent["style"] = struct{}{}
	p.setOfElementsToSkipContent["title"] = struct{}{}
}
