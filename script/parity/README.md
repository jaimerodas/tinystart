# The parity harness

The mechanical half of "it looks the same": the same sequence of requests
against the Rails app and against the Go binary, each with its own copy of the
same database, diffed byte for byte after a short list of normalisations.

```bash
script/parity/run                 # everything: ten seconds or so, 143 captures
script/parity/run -v              # …and print each diff
script/parity/run show edit       # …only these captures, from the last run
```

It exits non-zero if anything differs. The raw captures, the diffs, both
server logs and both databases stay under `tmp/parity/` afterwards:

```
tmp/parity/rails/003-passwords_new.txt   what Rails said
tmp/parity/go/003-passwords_new.txt      what the Go app said
tmp/parity/diffs/<name>.diff             what differed, if anything
tmp/parity/rails.log tmp/parity/go.log   the two servers
```

Not part of `script/test`: it needs Ruby, three ports and a Rails boot.

## What it does

1. Builds the Go binary and makes two copies of `storage/development.sqlite3` —
   the development database has richer data than the fixtures do: accents,
   emoji, three columns, a real connection — plus `seed.sql`, which adds the
   users that database has no reason to contain.
2. Starts `fake_app.py`: the connected app (device flow, federated search,
   visit) and Postmark, on one port, told apart by path. Nothing in a parity
   run reaches out of the machine.
3. Starts Rails through `serve.rb` on one port and the Go binary on another,
   each pointed at its own copy and at the fakes.
4. Runs `capture.py` against one and then the other. It is *one* sequence, in
   one file, run twice — which is what makes the captures comparable: the same
   requests in the same order against the same rows means the same ids on both
   sides.
5. Runs `diff.py`, which normalises and compares status, `Content-Type`,
   `Location`, `Content-Disposition` and body, and prints the table.

The last capture is the mailbox rather than a response: the password resets and
the admin's reset mail, as posted to the fake Postmark. Mail is the one thing
the app does that no page shows, and it is the same JSON to the same API on
both sides, so it diffs like a page.

The source database is never opened — both apps get a copy, `-wal` and all —
and the run fails if its fingerprint changed by the end of it, because that
file is somebody's working database.

## What is normalised, and what is not

Normalised (each is a difference that cannot reach a browser, and each is
commented where it happens in `diff.py`): asset fingerprints, 302 against 303,
absolute against relative `Location`, the body of a redirect, the CSRF meta
tags and hidden `authenticity_token` inputs, Rails' development view
annotations, the blank lines Rails' empty head helpers leave, `&#34;` against
`&quot;`, trailing whitespace, and the password reset token, which is signed
differently by each app and so can only be checked for being in the same place.

`Set-Cookie` is reported and never compared: the two apps set different cookies
by design — Rails a session cookie on every response, the Go app a signed flash
only when there is a flash — so the run prints the whole set from each side and
leaves the reading to a person.

Everything else that differs is a finding. **Do not add a normalisation to make
the harness pass**: fix the app, or, if the difference is real and harmless,
put it in `KNOWN` in `diff.py` with the sentence that justifies it, where it
prints on every run instead of disappearing.

## Notes

- Two things about `serve.rb`: it is not `bin/rails server` because half a dozen
  settings have to change before the middleware stack is built, and it turns
  forgery protection off, which is why Rails emits no CSRF tokens here.
- The export's filename carries the date in UTC. A run that crosses midnight
  UTC between the two captures will differ on the two export captures; run it
  again.
- Ports are 3097 (Rails), 3098 (the fakes) and 3099 (Go), and 3096 is left
  empty on purpose — it is the connected app that cannot be reached.
  `PARITY_RAILS_PORT`, `PARITY_FAKE_PORT`, `PARITY_GO_PORT`, `PARITY_OUT` and
  `PARITY_DB` override the defaults.

This whole directory dies with the Rails tree in phase 9: there will be nothing
left to compare against.
