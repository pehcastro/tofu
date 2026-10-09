(class_selector
  (class_name) @selector)

(id_selector
  (id_name) @selector)

(pseudo_class_selector
  (class_name) @selector.pseudo)

(pseudo_element_selector
  (tag_name) @selector.pseudo)

(plain_value) @constant

((plain_value) @variable
  (#match? @variable "^--"))

((property_name) @variable
  (#match? @variable "^--"))

(color_value) @string.special

(important) @keyword

(escape_sequence) @string.escape

[
  "("
  ")"
  "{"
  "}"
  "["
  "]"
] @punctuation.bracket

[
  ","
  ";"
  ":"
  "::"
] @punctuation.delimiter
