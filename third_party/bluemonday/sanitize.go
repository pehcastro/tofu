package bluemonday

import (
	"bytes"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/net/html"

	"github.com/aymerick/douceur/parser"
)

var (
	dataAttribute             = lazyRegexp("^data-.+")
	dataAttributeXMLPrefix    = lazyRegexp("^xml.+")
	dataAttributeInvalidChars = lazyRegexp("[A-Z;]+")
	cssUnicodeChar            = lazyRegexp(`\\[0-9a-f]{1,6} ?`)
	dataURIbase64Prefix       = lazyRegexp(`^data:[^,]*;base64,`)
)

func (p *Policy) Sanitize(s string) string {
	if strings.TrimSpace(s) == "" {
		return s
	}

	return p.sanitizeWithBuff(strings.NewReader(s)).String()
}

func (p *Policy) SanitizeBytes(b []byte) []byte {
	if len(bytes.TrimSpace(b)) == 0 {
		return b
	}

	return p.sanitizeWithBuff(bytes.NewReader(b)).Bytes()
}

func (p *Policy) SanitizeReader(r io.Reader) *bytes.Buffer {
	return p.sanitizeWithBuff(r)
}

func (p *Policy) SanitizeReaderToWriter(r io.Reader, w io.Writer) error {
	return p.sanitize(r, w)
}

type Query struct {
	Key      string
	Value    string
	HasValue bool
}

func parseQuery(query string) (values []Query, err error) {

	for query != "" {
		key := query
		if i := strings.IndexAny(key, "&;"); i >= 0 {
			key, query = key[:i], key[i+1:]
		} else {
			query = ""
		}
		if key == "" {
			continue
		}
		value := ""
		hasValue := false
		if i := strings.Index(key, "="); i >= 0 {
			key, value = key[:i], key[i+1:]
			hasValue = true
		}
		key, err1 := url.QueryUnescape(key)
		if err1 != nil {
			if err == nil {
				err = err1
			}
			continue
		}
		value, err1 = url.QueryUnescape(value)
		if err1 != nil {
			if err == nil {
				err = err1
			}
			continue
		}
		values = append(values, Query{
			Key:      key,
			Value:    value,
			HasValue: hasValue,
		})
	}
	return values, err
}

func encodeQueries(queries []Query) string {
	var buff bytes.Buffer
	for i, query := range queries {
		buff.WriteString(url.QueryEscape(query.Key))
		if query.HasValue {
			buff.WriteString("=")
			buff.WriteString(url.QueryEscape(query.Value))
		}
		if i < len(queries)-1 {
			buff.WriteString("&")
		}
	}
	return buff.String()
}

func sanitizedURL(val string) (string, error) {
	u, err := url.Parse(val)
	if err != nil {
		return "", err
	}

	queryValues, err := parseQuery(u.RawQuery)
	if err != nil {
		return "", err
	}

	for i, query := range queryValues {
		queryValues[i].Key = html.EscapeString(query.Key)
	}
	u.RawQuery = encodeQueries(queryValues)

	return u.String(), nil
}

func (p *Policy) sanitizeWithBuff(r io.Reader) *bytes.Buffer {
	var buff bytes.Buffer
	if err := p.sanitize(r, &buff); err != nil {
		return &bytes.Buffer{}
	}
	return &buff
}

type asStringWriter struct {
	io.Writer
}

func (a *asStringWriter) WriteString(s string) (int, error) {
	return a.Write([]byte(s))
}

func (p *Policy) sanitize(r io.Reader, w io.Writer) error {

	p.init()

	buff, ok := w.(stringWriterWriter)
	if !ok {
		buff = &asStringWriter{w}
	}

	var (
		skipElementContent       bool
		skippingElementsCount    int64
		skipClosingTag           bool
		closingTagToSkipStack    []string
		mostRecentlyStartedToken string
	)

	tokenizer := html.NewTokenizer(r)
	for {
		if tokenizer.Next() == html.ErrorToken {
			err := tokenizer.Err()
			if err == io.EOF {

				return nil
			}

			return err
		}

		token := tokenizer.Token()
		switch token.Type {
		case html.DoctypeToken:

		case html.CommentToken:

			if p.allowComments {

				buff.WriteString(token.String())
			}

		case html.StartTagToken:

			mostRecentlyStartedToken = normaliseElementName(token.Data)

			switch normaliseElementName(token.Data) {
			case `script`:
				if !p.allowUnsafe {
					continue
				}
			case `style`:
				if !p.allowUnsafe {
					continue
				}
			}

			aps, ok := p.elsAndAttrs[token.Data]
			if !ok {
				aa, matched := p.matchRegex(token.Data)
				if !matched {
					if _, ok := p.setOfElementsToSkipContent[token.Data]; ok {
						skipElementContent = true
						skippingElementsCount++
					}
					if p.addSpaces {
						if _, err := buff.WriteString(" "); err != nil {
							return err
						}
					}
					break
				}
				aps = aa
			}
			if len(token.Attr) != 0 {
				token.Attr = p.sanitizeAttrs(token.Data, token.Attr, aps)
			}

			if len(token.Attr) == 0 {
				if !p.allowNoAttrs(token.Data) {
					skipClosingTag = true
					closingTagToSkipStack = append(closingTagToSkipStack, token.Data)
					if p.addSpaces {
						if _, err := buff.WriteString(" "); err != nil {
							return err
						}
					}
					break
				}
			}

			if !skipElementContent {
				if _, err := buff.WriteString(token.String()); err != nil {
					return err
				}
			}

		case html.EndTagToken:

			if mostRecentlyStartedToken == normaliseElementName(token.Data) {
				mostRecentlyStartedToken = ""
			}

			switch normaliseElementName(token.Data) {
			case `script`:
				if !p.allowUnsafe {
					continue
				}
			case `style`:
				if !p.allowUnsafe {
					continue
				}
			}

			if skipClosingTag && closingTagToSkipStack[len(closingTagToSkipStack)-1] == token.Data {
				closingTagToSkipStack = closingTagToSkipStack[:len(closingTagToSkipStack)-1]
				if len(closingTagToSkipStack) == 0 {
					skipClosingTag = false
				}
				if p.addSpaces {
					if _, err := buff.WriteString(" "); err != nil {
						return err
					}
				}
				break
			}
			if _, ok := p.elsAndAttrs[token.Data]; !ok {
				match := false
				for regex := range p.elsMatchingAndAttrs {
					if regex.MatchString(token.Data) {
						skipElementContent = false
						match = true
						break
					}
				}
				if _, ok := p.setOfElementsToSkipContent[token.Data]; ok && !match {
					skippingElementsCount--
					if skippingElementsCount == 0 {
						skipElementContent = false
					}
				}
				if !match {
					if p.addSpaces {
						if _, err := buff.WriteString(" "); err != nil {
							return err
						}
					}
					break
				}
			}

			if !skipElementContent {
				if _, err := buff.WriteString(token.String()); err != nil {
					return err
				}
			}

		case html.SelfClosingTagToken:

			switch normaliseElementName(token.Data) {
			case `script`:
				if !p.allowUnsafe {
					continue
				}
			case `style`:
				if !p.allowUnsafe {
					continue
				}
			}

			aps, ok := p.elsAndAttrs[token.Data]
			if !ok {
				aa, matched := p.matchRegex(token.Data)
				if !matched {
					if p.addSpaces && !matched {
						if _, err := buff.WriteString(" "); err != nil {
							return err
						}
					}
					break
				}
				aps = aa
			}

			if len(token.Attr) != 0 {
				token.Attr = p.sanitizeAttrs(token.Data, token.Attr, aps)
			}

			if len(token.Attr) == 0 && !p.allowNoAttrs(token.Data) {
				if p.addSpaces {
					if _, err := buff.WriteString(" "); err != nil {
						return err
					}
				}
				break
			}
			if !skipElementContent {
				if _, err := buff.WriteString(token.String()); err != nil {
					return err
				}
			}

		case html.TextToken:

			if !skipElementContent {
				switch mostRecentlyStartedToken {
				case `script`:

					if p.allowUnsafe {
						if _, err := buff.WriteString(token.Data); err != nil {
							return err
						}
					}
				case "style":

					if p.allowUnsafe {
						if _, err := buff.WriteString(token.Data); err != nil {
							return err
						}
					}
				default:

					if _, err := buff.WriteString(token.String()); err != nil {
						return err
					}
				}
			}

		default:

			return fmt.Errorf("unknown token: %v", token)
		}
	}
}

func (p *Policy) sanitizeAttrs(
	elementName string,
	attrs []html.Attribute,
	aps map[string][]attrPolicy,
) []html.Attribute {

	if len(attrs) == 0 {
		return attrs
	}

	hasStylePolicies := false
	sps, elementHasStylePolicies := p.elsAndStyles[elementName]
	if len(p.globalStyles) > 0 || (elementHasStylePolicies && len(sps) > 0) {
		hasStylePolicies = true
	}

	if !hasStylePolicies {
		for k, v := range p.elsMatchingAndStyles {
			if k.MatchString(elementName) {
				if len(v) > 0 {
					hasStylePolicies = true
					break
				}
			}
		}
	}

	cleanAttrs := []html.Attribute{}
attrsLoop:
	for _, htmlAttr := range attrs {
		if p.allowDataAttributes {

			if isDataAttribute(htmlAttr.Key) {
				cleanAttrs = append(cleanAttrs, htmlAttr)
				continue
			}
		}

		if htmlAttr.Key == "style" && hasStylePolicies {
			htmlAttr = p.sanitizeStyles(htmlAttr, elementName)
			if htmlAttr.Val == "" {

				continue
			} else {
				cleanAttrs = append(cleanAttrs, htmlAttr)
				continue
			}
		}

		if apl, ok := aps[htmlAttr.Key]; ok {
			for _, ap := range apl {
				if ap.regexp != nil {
					if ap.regexp.MatchString(htmlAttr.Val) {
						cleanAttrs = append(cleanAttrs, htmlAttr)
						continue attrsLoop
					}
				} else {
					cleanAttrs = append(cleanAttrs, htmlAttr)
					continue attrsLoop
				}
			}
		}

		if apl, ok := p.globalAttrs[htmlAttr.Key]; ok {
			for _, ap := range apl {
				if ap.regexp != nil {
					if ap.regexp.MatchString(htmlAttr.Val) {
						cleanAttrs = append(cleanAttrs, htmlAttr)
						continue attrsLoop
					}
				} else {
					cleanAttrs = append(cleanAttrs, htmlAttr)
					continue attrsLoop
				}
			}
		}
	}

	if len(cleanAttrs) == 0 {

		return cleanAttrs
	}

	if linkable(elementName) {
		if p.requireParseableURLs {

			tmpAttrs := []html.Attribute{}
			for _, htmlAttr := range cleanAttrs {
				switch elementName {
				case "a", "area", "base", "link":
					if htmlAttr.Key == "href" {
						if u, ok := p.validURL(htmlAttr.Val); ok {
							htmlAttr.Val = u
							tmpAttrs = append(tmpAttrs, htmlAttr)
						}
						break
					}
					tmpAttrs = append(tmpAttrs, htmlAttr)
				case "blockquote", "del", "ins", "q":
					if htmlAttr.Key == "cite" {
						if u, ok := p.validURL(htmlAttr.Val); ok {
							htmlAttr.Val = u
							tmpAttrs = append(tmpAttrs, htmlAttr)
						}
						break
					}
					tmpAttrs = append(tmpAttrs, htmlAttr)
				case "audio", "embed", "iframe", "img", "script", "source", "track", "video":
					if htmlAttr.Key == "src" {
						if u, ok := p.validURL(htmlAttr.Val); ok {
							if p.srcRewriter != nil {
								parsedURL, err := url.Parse(u)
								if err != nil {
									fmt.Println(err)
								}
								p.srcRewriter(parsedURL)
								u = parsedURL.String()
							}
							htmlAttr.Val = u
							tmpAttrs = append(tmpAttrs, htmlAttr)
						}
						break
					}
					tmpAttrs = append(tmpAttrs, htmlAttr)
				default:
					tmpAttrs = append(tmpAttrs, htmlAttr)
				}
			}
			cleanAttrs = tmpAttrs
		}

		if (p.requireNoFollow ||
			p.requireNoFollowFullyQualifiedLinks ||
			p.requireNoReferrer ||
			p.requireNoReferrerFullyQualifiedLinks ||
			p.addTargetBlankToFullyQualifiedLinks) &&
			len(cleanAttrs) > 0 {

			switch elementName {
			case "a", "area", "base", "link":
				var hrefFound bool
				var externalLink bool
				for _, htmlAttr := range cleanAttrs {
					if htmlAttr.Key == "href" {
						hrefFound = true

						u, err := url.Parse(htmlAttr.Val)
						if err != nil {
							continue
						}
						if u.Host != "" {
							externalLink = true
						}

						continue
					}
				}

				if hrefFound {
					var (
						noFollowFound    bool
						noReferrerFound  bool
						targetBlankFound bool
					)

					addNoFollow := (p.requireNoFollow ||
						externalLink && p.requireNoFollowFullyQualifiedLinks)

					addNoReferrer := (p.requireNoReferrer ||
						externalLink && p.requireNoReferrerFullyQualifiedLinks)

					addTargetBlank := (externalLink &&
						p.addTargetBlankToFullyQualifiedLinks)

					tmpAttrs := []html.Attribute{}
					for _, htmlAttr := range cleanAttrs {

						var appended bool
						if htmlAttr.Key == "rel" && (addNoFollow || addNoReferrer) {

							if addNoFollow && !strings.Contains(htmlAttr.Val, "nofollow") {
								htmlAttr.Val += " nofollow"
							}
							if addNoReferrer && !strings.Contains(htmlAttr.Val, "noreferrer") {
								htmlAttr.Val += " noreferrer"
							}
							noFollowFound = addNoFollow
							noReferrerFound = addNoReferrer
							tmpAttrs = append(tmpAttrs, htmlAttr)
							appended = true
						}

						if elementName == "a" && htmlAttr.Key == "target" {
							if htmlAttr.Val == "_blank" {
								targetBlankFound = true
							}
							if addTargetBlank && !targetBlankFound {
								htmlAttr.Val = "_blank"
								targetBlankFound = true
								tmpAttrs = append(tmpAttrs, htmlAttr)
								appended = true
							}
						}

						if !appended {
							tmpAttrs = append(tmpAttrs, htmlAttr)
						}
					}
					if noFollowFound || noReferrerFound || targetBlankFound {
						cleanAttrs = tmpAttrs
					}

					if (addNoFollow && !noFollowFound) || (addNoReferrer && !noReferrerFound) {
						rel := html.Attribute{}
						rel.Key = "rel"
						if addNoFollow {
							rel.Val = "nofollow"
						}
						if addNoReferrer {
							if rel.Val != "" {
								rel.Val += " "
							}
							rel.Val += "noreferrer"
						}
						cleanAttrs = append(cleanAttrs, rel)
					}

					if elementName == "a" && addTargetBlank && !targetBlankFound {
						rel := html.Attribute{}
						rel.Key = "target"
						rel.Val = "_blank"
						targetBlankFound = true
						cleanAttrs = append(cleanAttrs, rel)
					}

					if targetBlankFound {

						var noOpenerAdded bool
						tmpAttrs := []html.Attribute{}
						for _, htmlAttr := range cleanAttrs {
							var appended bool
							if htmlAttr.Key == "rel" {
								if strings.Contains(htmlAttr.Val, "noopener") {
									noOpenerAdded = true
									tmpAttrs = append(tmpAttrs, htmlAttr)
								} else {
									htmlAttr.Val += " noopener"
									noOpenerAdded = true
									tmpAttrs = append(tmpAttrs, htmlAttr)
								}

								appended = true
							}
							if !appended {
								tmpAttrs = append(tmpAttrs, htmlAttr)
							}
						}
						if noOpenerAdded {
							cleanAttrs = tmpAttrs
						} else {

							rel := html.Attribute{}
							rel.Key = "rel"
							rel.Val = "noopener"
							cleanAttrs = append(cleanAttrs, rel)
						}

					}
				}
			default:
			}
		}
	}

	if p.requireCrossOriginAnonymous && len(cleanAttrs) > 0 {
		switch elementName {
		case "audio", "img", "link", "script", "video":
			var crossOriginFound bool
			for i, htmlAttr := range cleanAttrs {
				if htmlAttr.Key == "crossorigin" {
					crossOriginFound = true
					cleanAttrs[i].Val = "anonymous"
				}
			}

			if !crossOriginFound {
				crossOrigin := html.Attribute{}
				crossOrigin.Key = "crossorigin"
				crossOrigin.Val = "anonymous"
				cleanAttrs = append(cleanAttrs, crossOrigin)
			}
		}
	}

	if p.requireSandboxOnIFrame != nil && elementName == "iframe" {
		var sandboxFound bool
		for i, htmlAttr := range cleanAttrs {
			if htmlAttr.Key == "sandbox" {
				sandboxFound = true
				var cleanVals []string
				cleanValsSet := make(map[string]bool)
				for _, val := range strings.Fields(htmlAttr.Val) {
					if p.requireSandboxOnIFrame[val] {
						if !cleanValsSet[val] {
							cleanVals = append(cleanVals, val)
							cleanValsSet[val] = true
						}
					}
				}
				cleanAttrs[i].Val = strings.Join(cleanVals, " ")
			}
		}

		if !sandboxFound {
			sandbox := html.Attribute{}
			sandbox.Key = "sandbox"
			sandbox.Val = ""
			cleanAttrs = append(cleanAttrs, sandbox)
		}
	}

	return cleanAttrs
}

func (p *Policy) sanitizeStyles(attr html.Attribute, elementName string) html.Attribute {
	sps := p.elsAndStyles[elementName]
	if len(sps) == 0 {
		sps = map[string][]stylePolicy{}

		for regex, policies := range p.elsMatchingAndStyles {
			if regex.MatchString(elementName) {
				for k, v := range policies {
					sps[k] = append(sps[k], v...)
				}
			}
		}
	}

	attr.Val = strings.TrimRight(attr.Val, " ")
	if len(attr.Val) > 0 && attr.Val[len(attr.Val)-1] != ';' {
		attr.Val = attr.Val + ";"
	}
	decs, err := parser.ParseDeclarations(attr.Val)
	if err != nil {
		attr.Val = ""
		return attr
	}
	clean := []string{}
	prefixes := []string{"-webkit-", "-moz-", "-ms-", "-o-", "mso-", "-xv-", "-atsc-", "-wap-", "-khtml-", "prince-", "-ah-", "-hp-", "-ro-", "-rim-", "-tc-"}

decLoop:
	for _, dec := range decs {
		tempProperty := strings.ToLower(dec.Property)
		tempValue := removeUnicode(strings.ToLower(dec.Value))
		for _, i := range prefixes {
			tempProperty = strings.TrimPrefix(tempProperty, i)
		}
		if spl, ok := sps[tempProperty]; ok {
			for _, sp := range spl {
				if sp.handler != nil {
					if sp.handler(tempValue) {
						clean = append(clean, dec.Property+": "+dec.Value)
						continue decLoop
					}
				} else if len(sp.enum) > 0 {
					if stringInSlice(tempValue, sp.enum) {
						clean = append(clean, dec.Property+": "+dec.Value)
						continue decLoop
					}
				} else if sp.regexp != nil {
					if sp.regexp.MatchString(tempValue) {
						clean = append(clean, dec.Property+": "+dec.Value)
						continue decLoop
					}
				}
			}
		}
		if spl, ok := p.globalStyles[tempProperty]; ok {
			for _, sp := range spl {
				if sp.handler != nil {
					if sp.handler(tempValue) {
						clean = append(clean, dec.Property+": "+dec.Value)
						continue decLoop
					}
				} else if len(sp.enum) > 0 {
					if stringInSlice(tempValue, sp.enum) {
						clean = append(clean, dec.Property+": "+dec.Value)
						continue decLoop
					}
				} else if sp.regexp != nil {
					if sp.regexp.MatchString(tempValue) {
						clean = append(clean, dec.Property+": "+dec.Value)
						continue decLoop
					}
				}
			}
		}
	}
	if len(clean) > 0 {
		attr.Val = strings.Join(clean, "; ")
	} else {
		attr.Val = ""
	}
	return attr
}

func (p *Policy) allowNoAttrs(elementName string) bool {
	_, ok := p.setOfElementsAllowedWithoutAttrs[elementName]
	if !ok {
		for _, r := range p.setOfElementsMatchingAllowedWithoutAttrs {
			if r.MatchString(elementName) {
				ok = true
				break
			}
		}
	}
	return ok
}

func (p *Policy) validURL(rawurl string) (string, bool) {
	if p.requireParseableURLs {

		rawurl = strings.TrimSpace(rawurl)

		if strings.Contains(rawurl, " ") ||
			strings.Contains(rawurl, "\t") ||
			strings.Contains(rawurl, "\n") {
			if !strings.HasPrefix(rawurl, `data:`) {
				return "", false
			}

			matched := dataURIbase64Prefix().FindString(rawurl)
			if matched != "" {
				rawurl = matched + strings.Replace(
					strings.Replace(
						rawurl[len(matched):],
						"\r",
						"",
						-1,
					),
					"\n",
					"",
					-1,
				)
			}
		}

		u, err := url.Parse(rawurl)
		if err != nil {
			return "", false
		}

		if u.Scheme != "" {
			urlPolicies, ok := p.allowURLSchemes[u.Scheme]
			if !ok {
				for _, r := range p.allowURLSchemeRegexps {
					if r.MatchString(u.Scheme) {
						return u.String(), true
					}
				}

				return "", false
			}

			if len(urlPolicies) == 0 {
				return u.String(), true
			}

			for _, urlPolicy := range urlPolicies {
				if urlPolicy(u) {
					return u.String(), true
				}
			}

			return "", false
		}

		if p.allowRelativeURLs {
			if u.String() != "" {
				return u.String(), true
			}
		}

		return "", false
	}

	return rawurl, true
}

func linkable(elementName string) bool {
	switch elementName {
	case "a", "area", "base", "link":

		return true
	case "blockquote", "del", "ins", "q":

		return true
	case "audio", "embed", "iframe", "img", "input", "script", "track", "video":

		return true
	default:
		return false
	}
}

func stringInSlice(needle string, haystack []string) bool {
	for _, straw := range haystack {
		if strings.EqualFold(straw, needle) {
			return true
		}
	}
	return false
}

func isDataAttribute(val string) bool {
	if !dataAttribute().MatchString(val) {
		return false
	}
	rest := strings.Split(val, "data-")
	if len(rest) == 1 {
		return false
	}

	if dataAttributeXMLPrefix().MatchString(rest[1]) {
		return false
	}

	if dataAttributeInvalidChars().MatchString(rest[1]) {
		return false
	}
	return true
}

func removeUnicode(value string) string {
	substitutedValue := value
	currentLoc := cssUnicodeChar().FindStringIndex(substitutedValue)
	for currentLoc != nil {

		character := substitutedValue[currentLoc[0]+1 : currentLoc[1]]
		character = strings.TrimSpace(character)
		if len(character) < 4 {
			character = strings.Repeat("0", 4-len(character)) + character
		} else {
			for len(character) > 4 {
				if character[0] != '0' {
					character = ""
					break
				} else {
					character = character[1:]
				}
			}
		}
		character = "\\u" + character
		translatedChar, err := strconv.Unquote(`"` + character + `"`)
		translatedChar = strings.TrimSpace(translatedChar)
		if err != nil {
			return ""
		}
		substitutedValue = substitutedValue[0:currentLoc[0]] + translatedChar + substitutedValue[currentLoc[1]:]
		currentLoc = cssUnicodeChar().FindStringIndex(substitutedValue)
	}
	return substitutedValue
}

func (p *Policy) matchRegex(elementName string) (map[string][]attrPolicy, bool) {
	aps := make(map[string][]attrPolicy, 0)
	matched := false
	for regex, attrs := range p.elsMatchingAndAttrs {
		if regex.MatchString(elementName) {
			matched = true
			for k, v := range attrs {
				aps[k] = append(aps[k], v...)
			}
		}
	}
	return aps, matched
}

func normaliseElementName(str string) string {

	return strings.TrimSuffix(
		strings.TrimPrefix(
			strings.ToLower(
				strconv.QuoteToASCII(str),
			),
			`"`),
		`"`,
	)
}

type stringWriterWriter interface {
	io.Writer
	io.StringWriter
}
