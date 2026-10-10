(escape_sequence) @string.escape

(regex) @string.regex

[
  (true)
  (false)
] @boolean

[
  (null)
  (undefined)
] @constant

(this) @variable.special

(super) @variable.special

(shorthand_property_identifier) @property

(shorthand_property_identifier_pattern) @variable

(class_declaration
  name: (_) @type)

(new_expression
  constructor: (identifier) @type)

((comment) @comment.doc
  (#match? @comment.doc "^/\\*\\*"))

(template_substitution
  "${" @punctuation.special
  "}" @punctuation.special) @embedded
