[
  (integer_literal)
  (float_literal)
] @number

(boolean_literal) @boolean

(escape_sequence) @string.escape

(self) @variable.special

(shorthand_field_identifier) @property

(lifetime) @lifetime

(function_item
  name: (identifier) @function.definition)

(macro_definition
  name: (identifier) @function.special.definition)

(macro_invocation
  macro: (scoped_identifier
    name: (identifier) @function.special))

(macro_invocation
  macro: (identifier) @function.special
  "!" @function.special)

(enum_variant
  name: (identifier) @variant)

(mod_item
  name: (identifier) @namespace)

((scoped_identifier
  path: (identifier) @namespace)
  (#match? @namespace "^[a-z_]"))

((scoped_type_identifier
  path: (identifier) @namespace)
  (#match? @namespace "^[a-z_]"))

((scoped_use_list
  path: (identifier) @namespace)
  (#match? @namespace "^[a-z_]"))

((identifier) @constant
  (#match? @constant "^_*[A-Z][A-Z\\d_]+$"))

[
  "await"
  "break"
  "continue"
  "else"
  "if"
  "in"
  "loop"
  "match"
  "return"
  "while"
  "yield"
] @keyword.control

[
  "#"
  "?"
] @punctuation.special

(inner_attribute_item
  "!" @punctuation.special)

(attribute
  (identifier) @attribute)

(attribute
  (scoped_identifier
    name: (identifier) @attribute))
