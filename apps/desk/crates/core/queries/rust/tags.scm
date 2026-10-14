(function_item
  name: (identifier) @name) @definition.function

(function_signature_item
  name: (identifier) @name) @definition.function

(struct_item
  name: (type_identifier) @name) @definition.type

(enum_item
  name: (type_identifier) @name) @definition.type

(union_item
  name: (type_identifier) @name) @definition.type

(trait_item
  name: (type_identifier) @name) @definition.type

(type_item
  name: (type_identifier) @name) @definition.type

(enum_variant
  name: (identifier) @name) @definition.constant

(const_item
  name: (identifier) @name) @definition.constant

(static_item
  name: (identifier) @name) @definition.constant

(mod_item
  name: (identifier) @name) @definition.module

(macro_definition
  name: (identifier) @name) @definition.macro
