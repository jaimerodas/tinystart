#!/usr/bin/env python3
"""Drives one app through the parity sequence and writes down what it said.

One sequence, in one place, run twice — once against Rails and once against the
Go binary, each with its own copy of the same database. That is what makes the
captures comparable: the same requests in the same order against the same
starting rows means the same ids on both sides, so nothing has to be
renumbered afterwards.

    capture.py --base http://127.0.0.1:3097 --db a.sqlite3 \
               --fake http://127.0.0.1:3098 --out captures/rails

Each response becomes one file: the four headers worth comparing, the cookies
it set with their values redacted, then the body. diff.py does the rest.
"""

import argparse
import http.cookiejar
import json
import os
import re
import shutil
import sqlite3
import urllib.error
import urllib.parse
import urllib.request

# What Turbo puts on Accept when it submits a form. It is the switch the app
# runs on: with it a write answers with <turbo-stream> elements, without it
# with a redirect and a flash.
TURBO = "text/vnd.turbo-stream.html, text/html, application/xhtml+xml"

# A browser Rails' `allow_browser versions: :modern` gate will not turn away.
# Nothing renders the user agent, so its only job is to get past that.
BROWSER = ("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 "
           "(KHTML, like Gecko) Chrome/139.0.0.0 Safari/537.36")

# A port with nothing behind it: the connected app that cannot be reached.
DEAD = "http://127.0.0.1:3096"

PASSWORD = "password123"


class NoRedirects(urllib.request.HTTPRedirectHandler):
    """Stops urllib following a redirect.

    The redirect *is* the capture — its status, its Location and the flash it
    sets — so following it would throw away the thing being compared.
    """

    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


class Side:
    """One app, and the notebook the captures go in."""

    def __init__(self, base, db, fake, out):
        self.base = base.rstrip("/")
        self.db = db
        self.fake = fake.rstrip("/")
        self.out = out
        self.openers = {}
        self.step = 0
        # A fresh notebook every run: a capture left over from a sequence that
        # has since changed would be compared against nothing.
        shutil.rmtree(out, ignore_errors=True)
        os.makedirs(out)

    # --- the outside world -------------------------------------------------

    def opener(self, jar):
        """One cookie jar per browser. Three of them: the admin, somebody who
        is not an admin, and a visitor who has not signed in."""
        if jar not in self.openers:
            self.openers[jar] = urllib.request.build_opener(
                NoRedirects, urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
        return self.openers[jar]

    def sql(self, statement, *params):
        """A row the app has no page for. Both sides run it against their own
        database, so the two stay identical."""
        connection = sqlite3.connect(self.db, timeout=10)
        try:
            cursor = connection.execute(statement, params)
            rows = cursor.fetchall()
            connection.commit()
        finally:
            connection.close()
        return rows

    def value(self, statement, *params):
        return self.sql(statement, *params)[0][0]

    def mode(self, name):
        """What the fake connected app answers the next poll with."""
        self.post_raw(self.fake + "/_mode?mode=" + name)

    def reset_fake(self):
        self.post_raw(self.fake + "/_reset")

    def mails(self):
        with urllib.request.urlopen(self.fake + "/_mails", timeout=10) as response:
            return json.loads(response.read())

    def reset_token(self):
        """The password reset token out of the last mail sent.

        Each app signs its own — Rails a MessageVerifier blob, the Go app an
        HMAC — so neither can be handed the other's, and the harness has to
        read each side's out of the message it actually sent.
        """
        body = self.mails()[-1]["TextBody"]
        marker = "/passwords/"
        start = body.index(marker) + len(marker)
        return body[start:body.index("/edit", start)]

    @staticmethod
    def post_raw(url):
        request = urllib.request.Request(url, data=b"", method="POST")
        with urllib.request.urlopen(request, timeout=10) as response:
            response.read()

    # --- requests ----------------------------------------------------------

    def get(self, name, path, accept=None, jar="main"):
        return self.request(name, "GET", path, accept=accept, jar=jar)

    def post(self, name, path, data=None, files=None, accept=None, jar="main"):
        return self.request(name, "POST", path, data=data, files=files, accept=accept, jar=jar)

    def request(self, name, method, path, data=None, files=None, accept=None, jar="main"):
        headers = {"User-Agent": BROWSER}
        if accept:
            headers["Accept"] = accept

        body = None
        if files is not None:
            body, content_type = multipart(data or [], files)
            headers["Content-Type"] = content_type
        elif data is not None:
            body = urllib.parse.urlencode(data).encode()
            headers["Content-Type"] = "application/x-www-form-urlencoded"

        request = urllib.request.Request(self.base + path, data=body, headers=headers, method=method)
        try:
            response = self.opener(jar).open(request, timeout=30)
        except urllib.error.HTTPError as error:
            # A 3xx or a 4xx is an answer, not a failure: half the captures are
            # redirects and several are 404s and 422s.
            response = error
        with response:
            self.save(name, response)
        return response

    def save(self, name, response):
        headers = response.headers
        lines = [
            "STATUS %s" % response.status,
            "LOCATION %s" % (headers.get("Location") or ""),
            "TYPE %s" % (headers.get("Content-Type") or ""),
            "DISPOSITION %s" % (headers.get("Content-Disposition") or ""),
        ]
        # Cookies are compared as names and attributes only: the values are a
        # signature over a session id on one side and over another on the
        # other, and they could not match if the apps were identical.
        for cookie in headers.get_all("Set-Cookie") or []:
            lines.append("COOKIE %s" % redact_cookie(cookie))
        self.write(name, lines, response.read().decode("utf-8", "replace"), response.status)

    def save_mails(self, name):
        """The mailbox, as a capture of its own.

        Mail is the one thing the app does that no response shows, and it is
        the same JSON to the same API on both sides, so it diffs like a page.
        """
        body = json.dumps(self.mails(), indent=2, sort_keys=True, ensure_ascii=False)
        self.write(name, ["STATUS mailbox", "LOCATION ", "TYPE ", "DISPOSITION "], body, "mailbox")

    def write(self, name, lines, body, status):
        self.step += 1
        path = os.path.join(self.out, "%03d-%s.txt" % (self.step, name))
        with open(path, "w") as handle:
            handle.write("\n".join(lines) + "\n---\n" + body)
        print("%03d %-34s %s" % (self.step, name, status))


def redact_cookie(header):
    """`session_id=abc…; path=/; expires=…; HttpOnly` →
    `session_id=<value>; path=/; expires=<date>; HttpOnly`.

    The value is a signature and the expiry is thirty days from whenever the
    request happened, so neither could ever match between two runs, let alone
    two apps. The names and the rest of the attributes can.
    """
    name, _, rest = header.partition("=")
    _, _, attributes = rest.partition(";")
    attributes = re.sub(r"(?i)(expires=)[^;]+", r"\1<date>", attributes)
    return "%s=<value>%s" % (name, ";" + attributes if attributes else "")


def multipart(fields, files):
    """The one thing urllib will not build for you: a file upload."""
    boundary = "----parity"
    parts = []
    for key, value in fields:
        parts.append(('--%s\r\nContent-Disposition: form-data; name="%s"\r\n\r\n%s\r\n'
                      % (boundary, key, value)).encode())
    for key, filename, content_type, content in files:
        parts.append(('--%s\r\nContent-Disposition: form-data; name="%s"; filename="%s"\r\n'
                      'Content-Type: %s\r\n\r\n' % (boundary, key, filename, content_type)).encode())
        parts.append(content if isinstance(content, bytes) else content.encode())
        parts.append(b"\r\n")
    parts.append(("--%s--\r\n" % boundary).encode())
    return b"".join(parts), "multipart/form-data; boundary=" + boundary


def sequence(c, fixtures):
    """Every page in every reachable state, and every write, in one order.

    The order is load-bearing twice over: ids follow it, and so do the states —
    the connection is disconnected before the page that shows it disconnected,
    the start page is imported over after the editor has finished with it.
    """

    # --- state the development database does not have on its own ------------

    # The connection points at the real links.pati.to. Point it at the fake, so
    # that neither app reaches out of the machine.
    c.sql("UPDATE connections SET base_url = ?", c.fake)
    # A connection that has been refused: the reconnect notice on the start
    # page, which nothing in the sequence can produce on its own.
    c.sql("UPDATE connections SET last_error = 'links.pati.to rejected the token', "
          "last_failed_at = datetime('now', '-2 hours')")

    # --- signed out ---------------------------------------------------------

    c.get("session_new", "/session/new", jar="anon")
    c.get("sign_up", "/sign_up", jar="anon")
    c.get("passwords_new", "/passwords/new", jar="anon")
    c.get("root_signed_out", "/", jar="anon")
    c.get("settings_signed_out", "/settings", jar="anon")
    c.get("search_signed_out", "/search.json?q=alpha", jar="anon")
    c.post("visit_signed_out", "/visits", data={"link_id": "7"}, jar="anon")
    c.get("health", "/up", jar="anon")
    c.get("not_found", "/no-such-page", jar="anon")
    c.get("start_get", "/start", jar="anon")

    c.post("signin_bad", "/session", data={"email": "jaime@rodas.mx", "password": "wrong"}, jar="anon")
    c.get("session_new_alert", "/session/new", jar="anon")

    # --- the password reset, end to end -------------------------------------

    c.post("password_reset_unknown", "/passwords", data={"email": "nadie@example.com"}, jar="anon")
    c.post("password_reset_request", "/passwords", data={"email": "jaime@rodas.mx"}, jar="anon")
    c.get("session_new_reset_notice", "/session/new", jar="anon")

    token = c.reset_token()
    c.get("password_edit", "/passwords/%s/edit" % token, jar="anon")
    c.get("password_edit_invalid", "/passwords/not-a-token/edit", jar="anon")
    c.get("passwords_new_invalid_alert", "/passwords/new", jar="anon")

    c.post("password_reset_mismatch", "/passwords/%s" % token,
           data={"_method": "put", "password": PASSWORD, "password_confirmation": "otra"}, jar="anon")
    c.get("password_edit_alert", "/passwords/%s/edit" % token, jar="anon")
    # Reset to the same password it already was, so that everything below can
    # still sign in.
    c.post("password_reset_ok", "/passwords/%s" % token,
           data={"_method": "put", "password": PASSWORD, "password_confirmation": PASSWORD}, jar="anon")
    c.get("session_new_reset_ok", "/session/new", jar="anon")
    # The same link a second time: the digest it was signed against is gone.
    c.post("password_reset_reused", "/passwords/%s" % token,
           data={"_method": "put", "password": PASSWORD, "password_confirmation": PASSWORD}, jar="anon")
    c.get("passwords_new_reused_alert", "/passwords/new", jar="anon")

    # --- signing up ---------------------------------------------------------

    c.post("sign_up_taken", "/sign_up",
           data={"user[email]": "jaime@rodas.mx", "user[password]": PASSWORD}, jar="anon")
    c.post("sign_up_ok", "/sign_up",
           data={"user[email]": "nueva@example.com", "user[password]": PASSWORD}, jar="anon")
    # Two sign-ups in five minutes is the limit, on both sides.
    c.post("sign_up_rate_limited", "/sign_up",
           data={"user[email]": "otra@example.com", "user[password]": PASSWORD}, jar="anon")
    # Signed up but not approved: the sign-in form refuses.
    c.post("signin_unapproved", "/session",
           data={"email": "nueva@example.com", "password": PASSWORD}, jar="anon")

    # --- signed in ----------------------------------------------------------

    c.post("signin_ok", "/session", data={"email": "jaime@rodas.mx", "password": PASSWORD})
    c.get("show", "/")
    c.get("edit", "/start/edit")

    # --- the column count ---------------------------------------------------

    c.post("columns_ok_html", "/start", data={"_method": "patch", "user[columns]": "3"})
    c.post("columns_bad_stream", "/start", data={"_method": "patch", "user[columns]": "9"}, accept=TURBO)
    c.post("columns_bad_html", "/start", data={"_method": "patch", "user[columns]": "9"})
    # One column would strand the groups in columns two and three.
    c.post("columns_stranded_stream", "/start", data={"_method": "patch", "user[columns]": "1"}, accept=TURBO)
    c.post("columns_ok_stream", "/start", data={"_method": "patch", "user[columns]": "3"}, accept=TURBO)

    # --- groups -------------------------------------------------------------

    c.post("group_create_ok", "/start/groups",
           data={"start_page_group[name]": "Nuevo", "start_page_group[column]": "2"}, accept=TURBO)
    nuevo = c.value("SELECT id FROM start_page_groups WHERE name = 'Nuevo'")
    c.post("group_create_fail", "/start/groups",
           data={"start_page_group[name]": "Nuevo", "start_page_group[column]": "2"}, accept=TURBO)
    c.post("group_create_ok_html", "/start/groups",
           data={"start_page_group[name]": "Otro", "start_page_group[column]": "2"})
    otro = c.value("SELECT id FROM start_page_groups WHERE name = 'Otro'")

    c.post("group_update_ok", "/start/groups/%d" % nuevo,
           data={"_method": "patch", "start_page_group[name]": "Renombrado"}, accept=TURBO)
    c.post("group_update_fail", "/start/groups/%d" % nuevo,
           data={"_method": "patch", "start_page_group[name]": "Otro"}, accept=TURBO)
    # A name cleared to nothing: the field comes back with no value attribute.
    c.post("group_update_blank", "/start/groups/%d" % nuevo,
           data={"_method": "patch", "start_page_group[name]": ""}, accept=TURBO)
    c.post("group_update_fail_html", "/start/groups/%d" % nuevo,
           data={"_method": "patch", "start_page_group[name]": ""})

    c.post("group_move_same", "/start/groups/%d/move" % nuevo, data={"column": "2", "position": "0"}, accept=TURBO)
    c.post("group_move_other", "/start/groups/%d/move" % nuevo, data={"column": "1", "position": "0"}, accept=TURBO)
    c.post("group_move_refused", "/start/groups/%d/move" % nuevo, data={"column": "9", "position": "0"}, accept=TURBO)
    c.post("group_move_zero", "/start/groups/%d/move" % nuevo, data={"column": "0", "position": "0"}, accept=TURBO)
    c.post("group_move_refused_html", "/start/groups/%d/move" % nuevo, data={"column": "9", "position": "0"})

    # --- items --------------------------------------------------------------

    # The first group the database happens to have, rather than one named in
    # this file: the sequence has to run against whatever start page the
    # development database holds today.
    first = c.value("SELECT id FROM start_page_groups WHERE user_id = 1 "
                    "ORDER BY \"column\", position, id LIMIT 1")
    c.post("item_create_ok", "/start/items", accept=TURBO,
           data={"start_page_item[url]": "https://ejemplo.com/uno",
                 "start_page_item[title]": "Uno", "group_id": str(first)})
    uno = c.value("SELECT id FROM start_page_items WHERE url = 'https://ejemplo.com/uno'")
    c.post("item_create_fail", "/start/items", accept=TURBO,
           data={"start_page_item[url]": "https://ejemplo.com/uno",
                 "start_page_item[title]": "Dupe", "group_id": str(first)})
    c.post("item_create_fail_html", "/start/items",
           data={"start_page_item[url]": "no es url",
                 "start_page_item[title]": "Malo", "group_id": str(first)})
    # Both fields wrong at once: two error wrappers on one form.
    c.post("item_create_two_errors", "/start/items", accept=TURBO,
           data={"start_page_item[url]": "no es url",
                 "start_page_item[title]": "", "group_id": str(first)})

    c.post("item_update_ok", "/start/items/%d" % uno, accept=TURBO,
           data={"_method": "patch", "start_page_item[url]": "https://ejemplo.com/uno",
                 "start_page_item[title]": "Uno!"})
    c.post("item_update_fail", "/start/items/%d" % uno, accept=TURBO,
           data={"_method": "patch", "start_page_item[url]": "no es url",
                 "start_page_item[title]": "Uno!"})
    c.post("item_update_fail_html", "/start/items/%d" % uno,
           data={"_method": "patch", "start_page_item[url]": "no es url",
                 "start_page_item[title]": "Uno!"})

    c.post("item_move_same", "/start/items/%d/move" % uno, data={"position": "0"}, accept=TURBO)
    c.post("item_move_other", "/start/items/%d/move" % uno,
           data={"group_id": str(otro), "position": "0"}, accept=TURBO)
    # Refused, because the destination group already has that URL.
    c.sql("INSERT INTO start_page_items (start_page_group_id, title, url, position, visit_count, "
          "created_at, updated_at) VALUES (?, 'Copia', 'https://ejemplo.com/uno', 99, 0, "
          "'2026-08-15 12:00:00', '2026-08-15 12:00:00')", first)
    copia = c.value("SELECT id FROM start_page_items WHERE title = 'Copia'")
    c.post("item_move_refused", "/start/items/%d/move" % copia,
           data={"group_id": str(otro), "position": "0"}, accept=TURBO)
    c.post("item_move_refused_html", "/start/items/%d/move" % copia,
           data={"group_id": str(otro), "position": "0"})

    c.post("item_visit", "/start/items/%d/visit" % uno)

    c.post("item_destroy", "/start/items/%d" % uno, data={"_method": "delete"}, accept=TURBO)
    c.post("item_destroy_html", "/start/items/%d" % copia, data={"_method": "delete"})
    c.post("group_destroy", "/start/groups/%d" % nuevo, data={"_method": "delete"}, accept=TURBO)
    c.post("group_destroy_html", "/start/groups/%d" % otro, data={"_method": "delete"})

    c.get("edit_after_writes", "/start/edit")

    # --- settings -----------------------------------------------------------

    c.get("settings", "/settings")
    c.post("settings_update_ok", "/settings",
           data={"_method": "patch", "user[theme_preference]": "light", "user[color_preference]": "blue"})
    c.get("settings_notice", "/settings")
    c.post("settings_update_bad", "/settings", data={"_method": "patch", "user[theme_preference]": "neon"})
    c.get("settings_alert", "/settings")
    # The column count is the editor's; Settings must neither offer nor take it.
    c.post("settings_update_columns", "/settings",
           data={"_method": "patch", "user[columns]": "5", "user[theme_preference]": "dark"})
    c.post("settings_update_back", "/settings",
           data={"_method": "patch", "user[theme_preference]": "system", "user[color_preference]": "blue"})

    c.get("settings_password_edit", "/settings/password/edit")
    c.post("password_change_wrong", "/settings/password",
           data={"_method": "patch", "user[existing_password]": "wrong", "user[new_password]": "testtest"})
    c.post("password_change_blank", "/settings/password",
           data={"_method": "patch", "user[existing_password]": "", "user[new_password]": "testtest"})
    c.post("password_change_short", "/settings/password",
           data={"_method": "patch", "user[existing_password]": PASSWORD, "user[new_password]": "short"})

    # --- connections: the three states of the page --------------------------

    c.sql("DELETE FROM connections")
    c.get("connections_none", "/settings/connections")
    c.sql("INSERT INTO connections (user_id, base_url, token, scopes, token_expires_at, created_at, "
          "updated_at) VALUES (1, 'https://links.example.com', 'a-token', 'search,visit', "
          "datetime('now', '+25 days'), datetime('now'), datetime('now'))")
    c.get("connections_connected", "/settings/connections")
    c.sql("UPDATE connections SET last_error = 'links.example.com rejected the token — reconnect to "
          "restore search', last_failed_at = datetime('now') WHERE user_id = 1")
    c.get("connections_reconnect", "/settings/connections")
    c.get("show_reconnect", "/")
    c.sql("DELETE FROM connections")

    # --- connections: the flow, against the fake ----------------------------

    c.get("poll_idle", "/settings/connections/poll")
    c.mode("pending")
    c.post("connections_create", "/settings/connections", data={"base_url": c.fake})
    c.get("connections_pending", "/settings/connections")
    c.get("poll_pending", "/settings/connections/poll")
    c.mode("garbage")
    c.get("poll_unreachable", "/settings/connections/poll")
    c.mode("denied")
    c.get("poll_denied", "/settings/connections/poll")
    c.get("poll_idle_after_denial", "/settings/connections/poll")
    # A grant nobody approved in time. Like a denial, it ends the wait.
    c.mode("pending")
    c.post("connections_create_expiring", "/settings/connections", data={"base_url": c.fake})
    c.mode("expired")
    c.get("poll_expired", "/settings/connections/poll")
    c.get("poll_idle_after_expiry", "/settings/connections/poll")
    c.mode("pending")
    c.post("connections_create_again", "/settings/connections", data={"base_url": c.fake})
    c.mode("approved")
    c.get("poll_connected", "/settings/connections/poll")
    c.get("connections_after_approval", "/settings/connections")
    c.get("show_connected", "/")

    # --- search and visits, over the connection just approved ---------------

    c.get("search_results", "/search.json?q=alpha")
    # The JS asks for /search.json; Rails routed the resource, so /search
    # answers the same JSON. Both spellings are part of the surface.
    c.get("search_without_extension", "/search?q=alpha")
    c.get("search_blank", "/search.json?q=")
    c.get("search_no_param", "/search.json")
    c.post("visit_ok", "/visits", data={"link_id": "1"})

    c.post("connections_destroy", "/settings/connections", data={"_method": "delete"})
    c.get("connections_disconnected_notice", "/settings/connections")
    c.post("connections_create_unreachable", "/settings/connections", data={"base_url": DEAD})
    c.get("connections_unreachable_alert", "/settings/connections")
    c.get("search_disconnected", "/search.json?q=alpha")
    c.post("visit_disconnected", "/visits", data={"link_id": "7"})

    # --- import and export --------------------------------------------------

    c.get("import_export", "/settings/import_export")
    c.get("export", "/settings/export")

    c.post("import_no_file", "/settings/import_export", files=[])
    c.get("import_no_file_alert", "/settings/import_export")
    c.post("import_bad", "/settings/import_export",
           files=[("file", "start_page.yml", "text/yaml",
                   "1:\n- name: Broken\n  items:\n    Bare: example.com\n")])
    c.get("import_bad_alert", "/settings/import_export")
    c.post("import_too_large", "/settings/import_export",
           files=[("file", "start_page.yml", "text/yaml", "# padding\n" * 100_000)])
    c.get("import_too_large_alert", "/settings/import_export")
    c.post("import_not_utf8", "/settings/import_export",
           files=[("file", "start_page.yml", "text/yaml", b"1:\n- name: \xc3\x28Bad\n  items: {}\n")])
    c.get("import_not_utf8_alert", "/settings/import_export")
    c.post("import_ok", "/settings/import_export",
           files=[("file", "start_page.yml", "text/yaml", fixtures["start_page"])])
    c.get("import_ok_notice", "/settings/import_export")
    # The same file with a header that no longer counts what is under it: the
    # warning rides along with the success.
    c.post("import_warning", "/settings/import_export",
           files=[("file", "start_page.yml", "text/yaml",
                   fixtures["start_page"].replace("# 2 columns, 3 groups, 6 tiles",
                                                  "# 2 columns, 3 groups, 9 tiles"))])
    c.get("import_warning_notice", "/settings/import_export")
    c.get("export_after_import", "/settings/export")
    c.get("settings_after_import", "/settings")
    c.get("show_after_import", "/")
    c.get("edit_after_import", "/start/edit")

    # --- admin --------------------------------------------------------------

    pendiente = c.value("SELECT id FROM users WHERE email = 'pendiente@example.com'")
    c.get("admin_users", "/settings/admin/users")
    c.post("admin_approve", "/settings/admin/users/%d/approve" % pendiente)
    c.get("admin_users_after_approve", "/settings/admin/users")
    c.post("admin_reset", "/settings/admin/users/%d/password_reset" % pendiente)
    c.get("admin_reset_notice", "/settings/admin/users")
    c.post("admin_approve_back", "/settings/admin/users/%d/approve" % pendiente)

    # --- somebody who is not an admin ---------------------------------------

    c.post("signin_plain", "/session", data={"email": "vacio@example.com", "password": PASSWORD}, jar="plain")
    c.get("show_empty", "/", jar="plain")
    c.get("edit_empty", "/start/edit", jar="plain")
    c.get("settings_plain", "/settings", jar="plain")
    c.get("import_export_plain", "/settings/import_export", jar="plain")
    c.get("export_empty", "/settings/export", jar="plain")
    c.get("admin_users_denied", "/settings/admin/users", jar="plain")
    c.post("admin_approve_denied", "/settings/admin/users/%d/approve" % pendiente, jar="plain")
    c.post("admin_reset_denied", "/settings/admin/users/%d/password_reset" % pendiente, jar="plain")

    # --- the password change that works, and the way out --------------------

    c.post("password_change_ok", "/settings/password",
           data={"_method": "patch", "user[existing_password]": PASSWORD,
                 "user[new_password]": "testtesttest"})
    c.get("password_change_notice", "/settings")
    c.post("signout", "/session", data={"_method": "delete"})
    c.get("root_after_signout", "/")

    # Everything the run put in the post: the reset the visitor asked for and
    # the one the admin sent. Same API, same JSON, so it diffs like a page.
    c.save_mails("mails")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base", required=True, help="the app under capture")
    parser.add_argument("--db", required=True, help="its database, for the rows no page writes")
    parser.add_argument("--fake", required=True, help="the fake connected app and Postmark")
    parser.add_argument("--out", required=True, help="where the captures go")
    parser.add_argument("--fixture", required=True, help="test/fixtures/files/start_page.yml")
    args = parser.parse_args()

    with open(args.fixture) as handle:
        fixtures = {"start_page": handle.read()}

    side = Side(args.base, args.db, args.fake, args.out)
    # Each side starts the fake from the same place: no mail, flow pending.
    side.reset_fake()
    sequence(side, fixtures)
    print("%d captures in %s" % (side.step, args.out))


if __name__ == "__main__":
    main()
