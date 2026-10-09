(package_identifier) @namespace

(label_name) @label

(escape_sequence) @string.escape

[
  (true)
  (false)
] @boolean

(const_spec
  name: (identifier) @constant)

(method_elem
  name: (field_identifier) @function.method)

(keyed_element
  .
  (literal_element
    (identifier) @property))

(parameter_declaration
  name: (identifier) @variable.parameter)

(variadic_parameter_declaration
  name: (identifier) @variable.parameter)

(call_expression
  function: (identifier) @function)

(call_expression
  function: (selector_expression
    field: (field_identifier) @function.method))

((call_expression
  function: (identifier) @function.builtin)
  (#match? @function.builtin "^(append|cap|clear|close|complex|copy|delete|imag|len|make|max|min|new|panic|print|println|real|recover)$"))

(function_declaration
  name: (identifier) @function)

(method_declaration
  name: (field_identifier) @function.method)

((comment) @preproc
  (#match? @preproc "^//go:"))

[
  "("
  ")"
  "{"
  "}"
  "["
  "]"
] @punctuation.bracket

[
  "."
  ","
  ";"
  ":"
] @punctuation.delimiter
