# Migrations

`schema.sql` is the database as Rails left it. Each file here is one change
made after the rewrite.

To add a change, add a file named `<version>_<name>.sql`, where
`<version>` is a UTC timestamp in Rails' `YYYYMMDDHHMMSS` shape:

    20261101093000_add_pinned_to_start_page_items.sql

`Migrate` applies every file whose version is not already in
`schema_migrations`, in filename order, each one in its own transaction, and
records the version in that same table. That is the table Rails used, on
purpose: the production database already carries the eleven Rails versions,
and one ledger is better than two.

One statement per file is not required — the file is executed whole — but keep
each file to a single change, and never edit one that has already run anywhere.
