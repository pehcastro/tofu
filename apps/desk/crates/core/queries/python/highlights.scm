(escape_sequence) @string.escape

[
  (true)
  (false)
] @boolean

(none) @constant

((identifier) @variable.special
  (#match? @variable.special "^(self|cls)$"))

(parameters
  (identifier) @variable.parameter)

(default_parameter
  name: (identifier) @variable.parameter)

(typed_parameter
  (identifier) @variable.parameter)

(keyword_argument
  name: (identifier) @variable.parameter)

(class_definition
  name: (identifier) @type)

(function_definition
  name: (identifier) @function.definition)

(decorator
  "@" @punctuation.special)

(decorator
  (identifier) @attribute)

(decorator
  (call
    function: (identifier) @attribute))

(import_statement
  name: (dotted_name
    (identifier) @namespace))

(import_from_statement
  module_name: (dotted_name
    (identifier) @namespace))

(aliased_import
  alias: (identifier) @namespace)

(function_definition
  body: (block
    .
    (expression_statement
      (string) @comment.doc)))

(class_definition
  body: (block
    .
    (expression_statement
      (string) @comment.doc)))

(interpolation
  "{" @punctuation.special
  "}" @punctuation.special) @embedded

[
  "("
  ")"
  "["
  "]"
  "{"
  "}"
] @punctuation.bracket

[
  ","
  "."
  ":"
  ";"
] @punctuation.delimiter
