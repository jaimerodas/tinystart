# One user's credential for another app, obtained through that app's device
# flow (Settings → Connections).
#
# Scoped to a user on purpose: a token grants access to exactly one account on
# the other app, so it must only ever serve the person who approved it.
#
# Failures are recorded here so the start page can say "reconnect" instead of
# quietly showing no federated results — a lapsed token and an empty archive
# look identical otherwise.
class Connection < ApplicationRecord
  belongs_to :user

  validates :base_url, presence: true
  validates :token, presence: true
  validates :user_id, uniqueness: true

  # What the command bar calls the federated results: the bare host, no scheme.
  def hostname
    URI.parse(base_url).host
  rescue URI::InvalidURIError
    nil
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
