# library/qa

Shipped QA content, organised the way `.local/boji/planning/doing/library.md` draws it: a domain first, a language only when something in it is language-specific.

```
library/qa/agents/         a sub-agent definition
library/qa/references/     material any skill or agent in this domain may read
library/qa/general/rules/  rules true of testing in any language
library/qa/general/skills/ procedures true of testing in any language
```

Every file here declares `domain: qa`, and a file that does not is refused by name. A reference is reachable by every skill and every agent in this domain and by nothing else, which `internal/rule.LoadDomains` enforces rather than the convention hoping.

There is no `library/qa/go/`. Nothing in the twenty three skills read for TOFU-291 carries Go material: they are Playwright, Cypress, Jest, Selenium, React Testing Library, and three of them are not about software testing at all. Writing Go rules from memory instead is the mistake this domain was created to stop.

## Where the content came from

Every file here names the skill it was distilled from, by its path under `.local/sources/qa-skills/`. The clones are not shipped and not committed; the citation is so a person can read the original.

## The rule about rules

`general/rules/` holds five rules of two kinds.

**Two are `kind: measured`**, distilled from a QA skill. Each carries the `source:` it came from and `evidence:`, which is either a number from `bench/` or a written statement that there is none and why. Neither carries a `checker:`: they are measurements over a package across repeated runs, and the tool at `bench/testquality/flakerun` produces the number. `bench/testquality.QARuleFaults` is what refuses a rule distilled from a skill that cites no skill or carries no evidence.

**Three are `kind: structural`**, `test_assertion`, `test_boundary_cases` and `test_mock_boundary`. Each names a `checker:` builtin that parses Go and fires over a tree. They live in this domain rather than in `dev` because what they are about is test quality, and they live in `general/` rather than in a `go/` directory because what each one claims is true of testing in any language even though only the Go parser exists to check it.

All five are `mode: shadow`, which means they are recorded and not enforced, because a rule that has never been wrong about this repository has not earned the right to block anything.
