# the 9 over-cap read and search artifacts, judged against fixed truncation

generated 2026-09-23, corpus .tofu/sessions and .tofu/artifacts, real jev calls over library/questions/thrift@1.yaml, tool bench/thrift, run by TOFU_LIVE=1 go test ./bench/thrift/ -run TestTheNineOverCapArtifactsJudgedAgainstFixedTruncation -v -count=1.

Cost unit: money. A money figure is dollars that left the account behind the credential named above, read from the response of the call it names and never from a rate card.

this follows report-2026-09-23.md section 5, which sampled 8 results all under the truncation cap. This report reaches the other 9, the ones over it, ordered smallest paragraph count first so the call cap covers as many distinct artifacts as it can.

```

7. the 9 over-cap read and search artifacts, judged against fixed truncation on the part each one drops
9 distinct over-cap read or search artifacts identified from the corpus, 0 skipped before judging
call cap 250, 247 calls made, $0.013247 spent, 3 of 9 rows judged
turn                     tool      raw bytes  paras   fixed byt  fixed drop  fixed tokens  thrift byt thrift drop thrift tokens  overlap
turn-18d6ed674fe16fac    read          33549     73       32826         781          8206       32737        1244          8184        0
turn-18d73582f35a0ac8    search        45460     85       32828       12692          8207       26888       20405          6722     8052
turn-18d724d865ed9264    read          36807     89       32827        4039          8206       25387       13808          6346     3162
totals: raw 115816 bytes, fixed truncation 98481 bytes (24619 tokens, dropped 17512), thrift 85012 bytes (21252 tokens, dropped 35457), overlap between the two drops 11214 bytes
thrift cuts a further 3367 of 24619 fixed-truncation tokens, 13.7%, against the 1.5% mechanical floor from TOFU-338: this clears it
worked example: read read .local/boji/planning/boji-design-v0.md, turn turn-18d6ed674fe16fac: fixed truncation drops 781 bytes from the middle and thrift would have kept 781 of those bytes across 1 paragraph(s), so on this result truncation by position drops a part the judgment says still matters
cost against the first run: 247 calls here against 393 there, $0.013247 against $0.040968
  skipped: turn-18d72fd10b45a474: read 6de6161f084cada3e1c4231cccc62613: 89 paragraphs would exceed the 250 live call cap, 3 left
  skipped: turn-18d7404cb55ab0f4: read 0173237169a44e9cbaa1a35ee460439e: 95 paragraphs would exceed the 250 live call cap, 3 left
  skipped: turn-18d742bd38c430c4: read 5bce0918a7fd0a26320a61f695f929f4: 95 paragraphs would exceed the 250 live call cap, 3 left
  skipped: turn-18d6ea32da3230c0: read 249d425dcebbc379f72d1807402022ca: 798 paragraphs would exceed the 250 live call cap, 3 left
  skipped: turn-18d6ea32da3230c0: read 6acd9e5c2800f5cbf06751c0d6a21c08: 798 paragraphs would exceed the 250 live call cap, 3 left
  skipped: turn-18d6ed674fe16fac: read c93e40eff419f3127793499d763dc5d0: 843 paragraphs would exceed the 250 live call cap, 3 left
```
