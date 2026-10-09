
(type_identifier) @type

(predefined_type) @type.builtin

(interface_declaration
  name: (type_identifier) @type)

(type_alias_declaration
  name: (type_identifier) @type)

(enum_declaration
  name: (identifier) @enum)

(property_signature
  name: (property_identifier) @property)

(required_parameter
  pattern: (identifier) @variable.parameter)

(optional_parameter
  pattern: (identifier) @variable.parameter)

[
  "abstract"
  "declare"
  "enum"
  "implements"
  "interface"
  "keyof"
  "namespace"
  "private"
  "protected"
  "public"
  "readonly"
  "type"
] @keyword
