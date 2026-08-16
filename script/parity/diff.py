#!/usr/bin/env python3
"""Compares the two captures, after the normalisations the apps are allowed to
differ by — and only those.

The rule the harness lives by: a normalisation is a difference that cannot
reach a browser (a fingerprint in an asset name, a 302 where a 303 says the
same thing, one spelling of an escaped quote against another). Anything else
that differs is a finding, and the way to make it go away is to fix the app,
not to add a rule here.

    diff.py --rails captures/rails --go captures/go \\
            --rails-base http://127.0.0.1:3097 --go-base http://127.0.0.1:3099 \\
            --diffs captures/diffs [-v] [name…]
"""

import argparse
import difflib
import os
import re
import sys

# Differences that are real, are not normalisable, and are not the Go app being
# wrong. They print as their own row and are listed again at the end; they do
# not fail the run, and every one of them has to be defensible in a sentence.
KNOWN = {
    "health": "Rails' /up is rails/health#show, a green HTML page; the Go app "
              "answers `ok`. Same status, which is all kamal-proxy reads.",
    "not_found": "public/404.html byte for byte on both, but Rails' exception "
                 "middleware spells the charset UTF-8 and the Go app utf-8. "
                 "Case-insensitive by RFC 9110; matching it would mean one "
                 "page in the Go app spelling it differently from all others.",
    "start_get": "The same charset spelling as not_found: /start is a 404 on "
                 "purpose and both serve public/404.html to say so.",
    "mails": "Two differences, both inside the message rather than in what it "
             "says. postmark-rails sends a Headers array (Message-ID, "
             "MIME-Version, Content-Transfer-Encoding) that the Go client does "
             "not, and Postmark fills all three in itself. And html/template "
             "drops the CSS comment in the mail layout's empty <style> block: "
             "the template still carries it, the message loses it, and no "
             "recipient could see it either way. Anything else appearing in "
             "this diff is a finding.",
}


def split(text):
    """A capture is headers, then the cookies it set, then the body."""
    head, _, body = text.partition("\n---\n")
    lines = head.split("\n")
    headers = [line for line in lines if not line.startswith("COOKIE ")]
    cookies = [line for line in lines if line.startswith("COOKIE ")]
    return "\n".join(headers), cookies, body


def normalise(headers, body, base):
    text = headers + "\n---\n" + body

    # Propshaft folds the logical path into its digest; the Go side takes the
    # first eight hex of the SHA-256. Same shape, same place, same file.
    text = re.sub(r"-[0-9a-f]{8}\.(css|js)", r"-DIGEST.\1", text)

    # A redirect after a form post: Rails says 302, the Go app 303. Both mean
    # "now GET this"; 303 says it rather than relying on the habit.
    text = text.replace("STATUS 302", "STATUS 303")

    # Rails' redirects carry the full URL, the Go app only ever a path.
    text = text.replace(base, "")

    # The password reset token is signed differently on each side — a Rails
    # MessageVerifier blob against an HMAC — so the two can never match, and
    # the only thing worth comparing is that it is in the same place.
    text = re.sub(r"/passwords/[^/\"'\s<>]+/edit", "/passwords/TOKEN/edit", text)
    text = re.sub(r'(action|href)="/passwords/[^"]+"', r'\1="/passwords/TOKEN"', text)

    # ERB escapes a double quote as &quot;, html/template as &#34;. The same
    # character, and the DOM cannot tell them apart.
    text = text.replace("&#34;", "&quot;")

    # Rails in development annotates every rendered view. serve.rb turns that
    # off; this is here so a capture taken without it still compares.
    text = re.sub(r"^[ \t]*<!-- (BEGIN|END) app/views/.*?-->[ \t]*\n", "", text, flags=re.M | re.S)
    text = re.sub(r"<!-- (BEGIN|END) app/views/.*?-->", "", text, flags=re.S)

    # The CSRF token: two meta tags in <head> and a hidden input in every form.
    # The Go app has none — cross-origin protection needs no token — and
    # serve.rb turns forgery protection off so that Rails emits none either.
    text = re.sub(r'^[ \t]*<meta name="csrf-(param|token)"[^>]*>[ \t]*\n', "", text, flags=re.M)
    text = re.sub(r'<meta name="csrf-(param|token)"[^>]*>', "", text)
    text = re.sub(r'^[ \t]*<input type="hidden" name="authenticity_token"[^>]*>[ \t]*\n', "", text, flags=re.M)
    text = re.sub(r'<input type="hidden" name="authenticity_token"[^>]*(/>|>)', "", text)

    # Rails leaves a blank line in <head> wherever a helper produced nothing —
    # csrf_meta_tags, csp_meta_tag, yield :head, the commented-out PWA
    # manifest. The Go layout simply does not have those lines.
    head = re.search(r"<head>.*?</head>", text, re.S)
    if head:
        cleaned = re.sub(r"\n[ \t]*(?=\n)", "", head.group(0))
        text = text[:head.start()] + cleaned + text[head.end():]

    # Trailing whitespace: invisible in a browser, and left behind by the
    # substitutions above.
    text = re.sub(r"[ \t]+$", "", text, flags=re.M)

    if re.search(r"^STATUS 30\d$", text, re.M):
        # A redirect has no content type worth comparing — net/http writes one
        # only for a GET, Rails always — and no body either: net/http writes a
        # one-line courtesy page, Rails writes nothing, and no browser renders
        # either one. It follows the Location.
        text = re.sub(r"^TYPE .*$", "TYPE", text, flags=re.M)
        text = text[:text.index("---\n") + 4]

    return text


def main():
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--rails", required=True)
    parser.add_argument("--go", required=True)
    parser.add_argument("--rails-base", required=True)
    parser.add_argument("--go-base", required=True)
    parser.add_argument("--diffs", required=True, help="where the diffs of what differs are written")
    parser.add_argument("-v", "--verbose", action="store_true", help="print the diffs as well")
    parser.add_argument("names", nargs="*", help="only these captures")
    args = parser.parse_args()

    os.makedirs(args.diffs, exist_ok=True)
    for stale in os.listdir(args.diffs):
        os.remove(os.path.join(args.diffs, stale))

    files = sorted(f for f in os.listdir(args.rails) if f.endswith(".txt"))
    differ, known = [], []
    # Cookies are counted rather than listed per capture: the two apps set
    # different ones by design — Rails a session cookie on every response, the
    # Go app a signed flash only when there is a flash — so the interesting
    # thing is the whole set, not 138 copies of the same observation.
    cookies = {"rails": {}, "go": {}}
    cookie_captures = 0

    for filename in files:
        name = filename[4:-4] if re.match(r"\d{3}-", filename) else filename[:-4]
        if args.names and name not in args.names:
            continue

        with open(os.path.join(args.rails, filename)) as handle:
            rails_headers, rails_cookies, rails_body = split(handle.read())
        go_path = os.path.join(args.go, filename)
        if not os.path.exists(go_path):
            print("%-34s MISSING on the Go side" % name)
            differ.append(name)
            continue
        with open(go_path) as handle:
            go_headers, go_cookies, go_body = split(handle.read())

        for side, seen in (("rails", rails_cookies), ("go", go_cookies)):
            for cookie in seen:
                cookies[side][cookie] = cookies[side].get(cookie, 0) + 1
        if rails_cookies != go_cookies:
            cookie_captures += 1

        a = normalise(rails_headers, rails_body, args.rails_base)
        b = normalise(go_headers, go_body, args.go_base)

        if a == b:
            print("%-34s identical" % name)
            continue

        diff = list(difflib.unified_diff(a.splitlines(True), b.splitlines(True),
                                         "rails/" + name, "go/" + name, n=2))
        with open(os.path.join(args.diffs, name + ".diff"), "w") as handle:
            handle.writelines(diff)

        if name in KNOWN:
            known.append(name)
            print("%-34s known    %s" % (name, KNOWN[name]))
        else:
            differ.append(name)
            print("%-34s DIFF     (%d lines, %s)" % (name, len(diff),
                                                     os.path.join(args.diffs, name + ".diff")))
        if args.verbose:
            sys.stdout.writelines(diff[:200])

    print()
    if cookies["rails"] or cookies["go"]:
        print("Set-Cookie, values redacted — reported, never compared, never a failure:")
        for side in ("rails", "go"):
            for cookie, count in sorted(cookies[side].items()):
                print("  %-6s %-4d %s" % (side, count, cookie[len("COOKIE "):]))
        print("  the two sides set different cookies on %d of %d captures"
              % (cookie_captures, len(files)))
        print()
    if known:
        print("Known differences, left as findings rather than normalised away:")
        for name in known:
            print("  %s — %s" % (name, KNOWN[name]))
        print()

    total = len([f for f in files if not args.names or f[4:-4] in args.names])
    print("%d captures, %d identical, %d known, %d differing"
          % (total, total - len(differ) - len(known), len(known), len(differ)))
    return 1 if differ else 0


if __name__ == "__main__":
    sys.exit(main())
