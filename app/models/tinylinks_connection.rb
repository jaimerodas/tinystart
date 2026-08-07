# The single credential this app holds for tinylinks, obtained through its
# device flow (see `rails tinylinks:connect`).
#
# There is only ever one row. Failures are recorded on it so the start page can
# say "reconnect" instead of quietly showing no federated results — a lapsed
# token and an empty archive look identical otherwise.
class TinylinksConnection < ApplicationRecord
  validates :base_url, presence: true
  validates :token, presence: true

  def self.current
    order(:created_at).last
  end

  # A 401 or 403 means the token is gone or was revoked; nothing the app can do
  # but ask for a new one.
  def self.needs_reconnect?
    current.nil? || current.needs_reconnect?
  end

  def needs_reconnect?
    last_error.present?
  end

  def record_failure!(message)
    update_columns(last_failed_at: Time.current, last_error: message)
  end

  def clear_failure!
    return if last_error.nil? && last_failed_at.nil?

    update_columns(last_failed_at: nil, last_error: nil)
  end
end
