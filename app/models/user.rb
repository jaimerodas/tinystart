class User < ApplicationRecord
  attr_accessor :new_password, :existing_password

  VALID_COLORS = %w[red orange yellow green teal blue purple pink].freeze

  has_secure_password
  has_many :sessions, dependent: :destroy
  has_many :start_page_groups, dependent: :destroy
  has_many :start_page_items, through: :start_page_groups
  has_one :tinylinks_connection, dependent: :destroy

  validates :email, presence: true, uniqueness: true
  validates :theme_preference, inclusion: { in: %w[system light dark], message: "%{value} is not a valid theme" }
  validates :color_preference, inclusion: { in: VALID_COLORS, message: "%{value} is not a valid color" }
  validates :columns, presence: true,
            numericality: { only_integer: true, greater_than: 0, less_than_or_equal_to: 6 }

  validate :columns_leave_no_group_stranded

  normalizes :email, with: ->(e) { e.strip.downcase }

  before_create :bootstrap_first_user

  def groups_by_column
    start_page_groups.order(:position).group_by(&:column)
  end

  def column_range
    (1..columns).to_a
  end

  def groups_in_column(column_number)
    start_page_groups.in_column(column_number).ordered
  end

  # Embedded in the page so the command bar can filter tiles without a round trip.
  def links_for_command_bar
    start_page_items.map do |item|
      { title: item.title, url: item.url, id: item.id }
    end
  end

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

  # The grid only renders column_range, so a group left beyond the limit would
  # disappear from the start page and the edit page both, taking its move and
  # delete buttons with it. Refuse the change rather than hide someone's work.
  def columns_leave_no_group_stranded
    return unless persisted? && columns.present? && columns_changed?

    stranded = start_page_groups.where("\"column\" > ?", columns).order(:column)
    return if stranded.empty?

    names = stranded.map { |group| "\"#{group.name}\"" }.to_sentence
    errors.add(:columns, "can't be fewer than #{stranded.maximum(:column)} — " \
                         "that would hide #{names}. Move them first.")
  end

  def bootstrap_first_user
    return if User.exists?
    self.approved = true
    self.admin = true
  end
end
