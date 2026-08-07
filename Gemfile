source "https://rubygems.org"

gem "rails", "~> 8.1"
gem "propshaft"
gem "sqlite3"
gem "puma"
gem "importmap-rails"
gem "turbo-rails"
gem "stimulus-rails"
gem "bcrypt", "~> 3.1.7"
gem "tzinfo-data", platforms: %i[ windows jruby ]
gem "solid_cache"
gem "bootsnap", require: false
gem "kamal", ">= 2", require: false
gem "thruster", require: false
gem "postmark-rails"
gem "device_detector"

# Needed on Ruby 4 so Kamal's ssh (net-ssh) can negotiate with the server — not used
# directly by the app, don't remove it
gem "openssl"

group :development, :test do
  gem "debug", platforms: %i[ mri windows ], require: "debug/prelude"
  gem "brakeman", require: false
  gem "rubocop-rails-omakase", require: false
  gem "dotenv-rails"
end

group :development do
  gem "web-console"
end

group :test do
  gem "mocha"
  gem "capybara"
  gem "selenium-webdriver"
  gem "simplecov"
  gem "minitest-mock"
end
