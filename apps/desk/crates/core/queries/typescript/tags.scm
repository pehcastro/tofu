(function_declaration
  name: (identifier) @name) @definition.function

(generator_function_declaration
  name: (identifier) @name) @definition.function

(function_signature
  name: (identifier) @name) @definition.function

(class_declaration
  name: (type_identifier) @name) @definition.type

(abstract_class_declaration
  name: (type_identifier) @name) @definition.type

(interface_declaration
  name: (type_identifier) @name) @definition.type

(type_alias_declaration
  name: (type_identifier) @name) @definition.type

(enum_declaration
  name: (identifier) @name) @definition.type

(method_definition
  name: (property_identifier) @name) @definition.method

(abstract_method_signature
  name: (property_identifier) @name) @definition.method

(internal_module
  name: (identifier) @name) @definition.module

(program
  (lexical_declaration
    (variable_declarator
      name: (identifier) @name) @definition.constant))

(export_statement
  (lexical_declaration
    (variable_declarator
      name: (identifier) @name) @definition.constant))
