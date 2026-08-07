class User < ApplicationRecord
  attr_accessor :new_password, :existing_password

  VALID_COLORS = %w[red orange yellow green teal blue purple pink gray].freeze

  has_secure_password
  has_many :sessions, dependent: :destroy

  validates :email, presence: true, uniqueness: true
  validates :theme_preference, inclusion: { in: %w[system light dark], message: "%{value} is not a valid theme" }
  validates :color_preference, inclusion: { in: VALID_COLORS, message: "%{value} is not a valid color" }

  normalizes :email, with: ->(e) { e.strip.downcase }

  before_create :bootstrap_first_user

  def update_password(params)
    assign_attributes(params)
    errors.add(:existing_password, "can't be blank") and return unless existing_password.present?
    errors.add(:existing_password, "is incorrect") and return unless authenticate(existing_password)
    errors.add(:new_password, "has to be longer") and return unless validate_new_password
    self.password = new_password
    save
  end

  private

  def validate_new_password
    new_password.present? && new_password.length > 6
  end

  def bootstrap_first_user
    return if User.exists?
    self.approved = true
    self.admin = true
  end
end
