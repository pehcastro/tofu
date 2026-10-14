use desk_markdown::{Options, parse, to_html};

const CASES: [(&str, &str); 11] = [
    (
        "inline $x^2$ and prices $5 and $6.50.\n$$\\frac{a}{b} + \\sqrt{x}$$",
        "<p>inline <span class=\"math inline\">\\(x^2\\)</span> and prices $5 and $6.50.\n<span class=\"math display\">\\[\\frac{a}{b} + \\sqrt{x}\\]</span></p>\n",
    ),
    (
        "inline $x^2$ here",
        "<p>inline <span class=\"math inline\">\\(x^2\\)</span> here</p>\n",
    ),
    ("costs $5 and $10", "<p>costs $5 and $10</p>\n"),
    ("from $5 to $6.50 now", "<p>from $5 to $6.50 now</p>\n"),
    ("a $ b$ c", "<p>a $ b$ c</p>\n"),
    ("unclosed $x", "<p>unclosed $x</p>\n"),
    (
        "escaped $a\\$b$",
        "<p>escaped <span class=\"math inline\">\\(a\\$b\\)</span></p>\n",
    ),
    (
        "$$\\frac{a}{b}$$",
        "<div class=\"math display\">\\[\\frac{a}{b}\\]</div>\n",
    ),
    (
        "$$\n\\sum_i x_i\n$$",
        "<div class=\"math display\">\\[\\sum_i x_i\\]</div>\n",
    ),
    (
        "```math\nE = mc^2\n```",
        "<div class=\"math display\">\\[E = mc^2\\]</div>\n",
    ),
    ("`$x$` stays code", "<p><code>$x$</code> stays code</p>\n"),
];

#[test]
fn math_on_parses_dollars_pandoc_style() {
    let failed: Vec<_> = CASES
        .iter()
        .map(|(md, want)| (md, want, to_html(&parse(md, Options::ALL))))
        .filter(|(_, want, got)| got != *want)
        .collect();
    assert!(failed.is_empty(), "{failed:#?}");
}

#[test]
fn math_off_leaves_dollars_as_text() {
    let got = to_html(&parse("inline $x^2$ and $$y$$", Options::GFM));
    assert_eq!(got, "<p>inline $x^2$ and $$y$$</p>\n");
}
