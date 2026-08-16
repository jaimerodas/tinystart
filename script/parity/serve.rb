#!/usr/bin/env ruby
# The Rails app on a port, for the parity harness.
#
# Not `bin/rails server`, because half a dozen settings have to be different
# before the middleware stack is built and there is no environment variable for
# any of them. Requiring config/application loads the configuration without
# running it; the overrides go in an initializer of the application's own,
# which runs after config/environments/development.rb has had its say and
# before the middleware stack is built.
#
#   PARITY_PORT=3097 DATABASE_URL=sqlite3:<copy> PARITY_FAKE=127.0.0.1:3098 \
#     bundle exec ruby script/parity/serve.rb
#
# Dies with the Rails tree in phase 9.

require_relative "../../config/application"

port = Integer(ENV.fetch("PARITY_PORT", "3097"))
fake_host, fake_port = ENV.fetch("PARITY_FAKE", "127.0.0.1:3098").split(":")

Rails.application.class.initializer "parity.overrides" do |app|
  config = app.config
  # The harness talks to 127.0.0.1, which the development host guard would
  # otherwise send a "Blocked host" page instead of the app.
  config.hosts.clear

  # Development renders its own debug page for a 404 and a 500; production
  # serves public/404.html, which is what the Go app serves everywhere. Without
  # this the two sides would differ on every not-found by design, and the
  # difference would be Rails' debugger rather than the app.
  config.consider_all_requests_local = false

  # No CSRF token in the markup and none demanded of the harness. The Go app
  # has no tokens at all — it relies on Sec-Fetch-Site — so this is the
  # comparison the normaliser would otherwise have to fake up by stripping
  # csrf_meta_tags and every hidden authenticity_token input.
  config.action_controller.allow_forgery_protection = false

  # <!-- BEGIN app/views/… --> around every partial. The normaliser strips them
  # too, belt and braces, but not emitting them keeps the raw captures readable.
  config.action_view.annotate_rendered_view_with_filenames = false

  # Nothing changes under the harness, and reloading makes the first request
  # after each file check slower and the timings noisier.
  config.enable_reloading = false
  config.server_timing = false

  # Mail goes to the fake Postmark on the same port as the fake connected app —
  # they are told apart by path. Development's letter_opener writes a file and
  # opens a browser tab, and the harness needs to read the reset link back out
  # of the message anyway, which it does through the fake.
  config.action_mailer.delivery_method = :postmark
  config.action_mailer.postmark_settings = {
    api_token: "parity-fake-token",
    host: fake_host,
    port: Integer(fake_port),
    secure: false
  }
  config.action_mailer.default_url_options = { host: "127.0.0.1", port: port }
end

Rails.application.initialize!

require "puma"

server = Puma::Server.new(Rails.application)
server.add_tcp_listener("127.0.0.1", port)
warn "rails listening on 127.0.0.1:#{port}"
server.run.join
