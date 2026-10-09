(variable_name) @variable

(special_variable_name) @variable.special

(number) @number

(test_operator) @operator

(heredoc_start) @label

(heredoc_end) @label

(simple_expansion
  "$" @punctuation.special)

(expansion
  "${" @punctuation.special
  "}" @punctuation.special)

(command_substitution
  "$(" @punctuation.special
  ")" @punctuation.special)

((program
  .
  (comment) @preproc)
  (#match? @preproc "^#!"))

[
  "("
  ")"
  "{"
  "}"
  "["
  "]"
  "[["
  "]]"
] @punctuation.bracket

[
  ";"
  ";;"
] @punctuation.delimiter
