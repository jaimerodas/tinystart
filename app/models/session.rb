class Session < ApplicationRecord
  belongs_to :user

  SESSION_LIFETIME = 30.days

  scope :expired, -> { where("expires_at < ?", Time.current) }
  scope :active, -> { where("expires_at >= ?", Time.current) }

  def expired?
    expires_at < Time.current
  end

  def extend_session!
    update!(expires_at: SESSION_LIFETIME.from_now)
  end

  after_create :set_initial_expiration

  def device_info
    @device_info ||= DeviceDetector.new(user_agent)
  end

  def device_name
    return "Unknown Device" if !device_info.known?
    device_info.device_name.presence || device_info.device_type&.humanize || "Computer"
  end

  def browser
    return "Unknown Browser" if !device_info.known?
    "#{device_info.name} #{device_info.full_version}"
  end

  def os
    return "Unknown OS" if !device_info.known?
    "#{device_info.os_name} #{device_info.os_full_version}"
  end

  def formatted_device_info
    parts = []

    # Add device name (e.g., "iPhone", "MacBook", "Computer")
    parts << device_name

    # Add browser with version if available
    parts << browser

    # Add OS
    parts << os

    parts.join(" • ")
  end

  private

  def set_initial_expiration
    update!(expires_at: SESSION_LIFETIME.from_now) if expires_at.nil?
  end
end
