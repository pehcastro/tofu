package chroma

import "sync"

//go:generate enumer -text -type TokenType

type TokenType int

const (
	Background TokenType = -1 - iota

	PreWrapper

	Line

	LineNumbers

	LineNumbersTable

	LineHighlight

	LineTable

	LineTableTD

	LineLink

	CodeLine

	Error

	Other

	None

	Ignore

	EOFType TokenType = 0
)

const (
	Keyword TokenType = 1000 + iota
	KeywordConstant
	KeywordDeclaration
	KeywordNamespace
	KeywordPseudo
	KeywordReserved
	KeywordType
)

const (
	Name TokenType = 2000 + iota
	NameAttribute
	NameClass
	NameConstant
	NameDecorator
	NameEntity
	NameException
	NameKeyword
	NameLabel
	NameNamespace
	NameOperator
	NameOther
	NamePseudo
	NameProperty
	NameTag
)

const (
	NameBuiltin TokenType = 2100 + iota
	NameBuiltinPseudo
)

const (
	NameVariable TokenType = 2200 + iota
	NameVariableAnonymous
	NameVariableClass
	NameVariableGlobal
	NameVariableInstance
	NameVariableMagic
)

const (
	NameFunction TokenType = 2300 + iota
	NameFunctionMagic
)

const (
	Literal TokenType = 3000 + iota
	LiteralDate
	LiteralOther
)

const (
	LiteralString TokenType = 3100 + iota
	LiteralStringAffix
	LiteralStringAtom
	LiteralStringBacktick
	LiteralStringBoolean
	LiteralStringChar
	LiteralStringDelimiter
	LiteralStringDoc
	LiteralStringDouble
	LiteralStringEscape
	LiteralStringHeredoc
	LiteralStringInterpol
	LiteralStringName
	LiteralStringOther
	LiteralStringRegex
	LiteralStringSingle
	LiteralStringSymbol
)

const (
	LiteralNumber TokenType = 3200 + iota
	LiteralNumberBin
	LiteralNumberFloat
	LiteralNumberHex
	LiteralNumberInteger
	LiteralNumberIntegerLong
	LiteralNumberOct
	LiteralNumberByte
)

const (
	Operator TokenType = 4000 + iota
	OperatorWord
	OperatorReserved
)

const (
	Punctuation TokenType = 5000 + iota
)

const (
	Comment TokenType = 6000 + iota
	CommentHashbang
	CommentMultiline
	CommentSingle
	CommentSpecial
)

const (
	CommentPreproc TokenType = 6100 + iota
	CommentPreprocFile
)

const (
	Generic TokenType = 7000 + iota
	GenericDeleted
	GenericEmph
	GenericError
	GenericHeading
	GenericInserted
	GenericOutput
	GenericPrompt
	GenericStrong
	GenericSubheading
	GenericTraceback
	GenericUnderline
)

const (
	Text TokenType = 8000 + iota
	TextWhitespace
	TextSymbol
	TextPunctuation
)

const (
	Whitespace = TextWhitespace

	Date = LiteralDate

	String          = LiteralString
	StringAffix     = LiteralStringAffix
	StringBacktick  = LiteralStringBacktick
	StringChar      = LiteralStringChar
	StringDelimiter = LiteralStringDelimiter
	StringDoc       = LiteralStringDoc
	StringDouble    = LiteralStringDouble
	StringEscape    = LiteralStringEscape
	StringHeredoc   = LiteralStringHeredoc
	StringInterpol  = LiteralStringInterpol
	StringOther     = LiteralStringOther
	StringRegex     = LiteralStringRegex
	StringSingle    = LiteralStringSingle
	StringSymbol    = LiteralStringSymbol

	Number            = LiteralNumber
	NumberBin         = LiteralNumberBin
	NumberFloat       = LiteralNumberFloat
	NumberHex         = LiteralNumberHex
	NumberInteger     = LiteralNumberInteger
	NumberIntegerLong = LiteralNumberIntegerLong
	NumberOct         = LiteralNumberOct
)

var StandardTypes = sync.OnceValue(func() map[TokenType]string {
	return map[TokenType]string{
		Background:       "bg",
		PreWrapper:       "chroma",
		Line:             "line",
		LineNumbers:      "ln",
		LineNumbersTable: "lnt",
		LineHighlight:    "hl",
		LineTable:        "lntable",
		LineTableTD:      "lntd",
		LineLink:         "lnlinks",
		CodeLine:         "cl",
		Text:             "",
		Whitespace:       "w",
		Error:            "err",
		Other:            "x",

		Keyword:            "k",
		KeywordConstant:    "kc",
		KeywordDeclaration: "kd",
		KeywordNamespace:   "kn",
		KeywordPseudo:      "kp",
		KeywordReserved:    "kr",
		KeywordType:        "kt",

		Name:                 "n",
		NameAttribute:        "na",
		NameBuiltin:          "nb",
		NameBuiltinPseudo:    "bp",
		NameClass:            "nc",
		NameConstant:         "no",
		NameDecorator:        "nd",
		NameEntity:           "ni",
		NameException:        "ne",
		NameFunction:         "nf",
		NameFunctionMagic:    "fm",
		NameProperty:         "py",
		NameLabel:            "nl",
		NameNamespace:        "nn",
		NameOther:            "nx",
		NameTag:              "nt",
		NameVariable:         "nv",
		NameVariableClass:    "vc",
		NameVariableGlobal:   "vg",
		NameVariableInstance: "vi",
		NameVariableMagic:    "vm",

		Literal:     "l",
		LiteralDate: "ld",

		String:          "s",
		StringAffix:     "sa",
		StringBacktick:  "sb",
		StringChar:      "sc",
		StringDelimiter: "dl",
		StringDoc:       "sd",
		StringDouble:    "s2",
		StringEscape:    "se",
		StringHeredoc:   "sh",
		StringInterpol:  "si",
		StringOther:     "sx",
		StringRegex:     "sr",
		StringSingle:    "s1",
		StringSymbol:    "ss",

		Number:            "m",
		NumberBin:         "mb",
		NumberFloat:       "mf",
		NumberHex:         "mh",
		NumberInteger:     "mi",
		NumberIntegerLong: "il",
		NumberOct:         "mo",

		Operator:         "o",
		OperatorWord:     "ow",
		OperatorReserved: "or",

		Punctuation: "p",

		Comment:            "c",
		CommentHashbang:    "ch",
		CommentMultiline:   "cm",
		CommentPreproc:     "cp",
		CommentPreprocFile: "cpf",
		CommentSingle:      "c1",
		CommentSpecial:     "cs",

		Generic:           "g",
		GenericDeleted:    "gd",
		GenericEmph:       "ge",
		GenericError:      "gr",
		GenericHeading:    "gh",
		GenericInserted:   "gi",
		GenericOutput:     "go",
		GenericPrompt:     "gp",
		GenericStrong:     "gs",
		GenericSubheading: "gu",
		GenericTraceback:  "gt",
		GenericUnderline:  "gl",
	}
})

func (t TokenType) Parent() TokenType {
	if t%100 != 0 {
		return t / 100 * 100
	}
	if t%1000 != 0 {
		return t / 1000 * 1000
	}
	return 0
}

func (t TokenType) Category() TokenType {
	return t / 1000 * 1000
}

func (t TokenType) SubCategory() TokenType {
	return t / 100 * 100
}

func (t TokenType) InCategory(other TokenType) bool {
	return t/1000 == other/1000
}

func (t TokenType) InSubCategory(other TokenType) bool {
	return t/100 == other/100
}

func (t TokenType) Emit(groups []string, _ *LexerState) Iterator {
	return Literator(Token{Type: t, Value: groups[0]})
}

func (t TokenType) EmitterKind() string { return "token" }
