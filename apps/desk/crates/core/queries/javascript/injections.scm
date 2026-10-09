(call_expression
  function: (member_expression
    object: (identifier) @_styled
    (#eq? @_styled "styled"))
  arguments: (template_string
    (string_fragment) @injection.content)
  (#set! injection.language "css")
  (#set! injection.combined)
  (#set! injection.include-children))

(call_expression
  function: (identifier) @_css
  (#eq? @_css "css")
  arguments: (template_string
    (string_fragment) @injection.content)
  (#set! injection.language "css")
  (#set! injection.combined)
  (#set! injection.include-children))

(call_expression
  function: (call_expression
    function: (identifier) @_styled
    (#eq? @_styled "styled"))
  arguments: (template_string
    (string_fragment) @injection.content)
  (#set! injection.language "css")
  (#set! injection.combined)
  (#set! injection.include-children))
