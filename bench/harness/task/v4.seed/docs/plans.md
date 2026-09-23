# Plans

Last touched for the 3.2 release.

## free

Read your own usage and pull a CSV of it. One seat, 50,000 rows a month. No scheduled reports.

## silver

Everything on free, plus scheduled reports and a JSON export. Five seats, 500,000 rows a month.

## gold

Everything the product does. Every export format, every report, twenty seats, five million rows a month. There is no capability on gold that has to be asked for.

## enterprise

Gold, with the seat count and the row ceiling negotiated per contract, and a support line.

## Changing a plan

Plans are a table in the source, not a database row. Changing what a plan carries is a code change and a release. Ask before you do it: a customer on gold has been sold the whole product and taking something back out of it is a conversation with their account manager, not a pull request.
