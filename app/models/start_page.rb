class StartPage < ApplicationRecord
  belongs_to :user
  has_many :start_page_groups, dependent: :destroy
  has_many :start_page_items, through: :start_page_groups

  validates :name, presence: true
  validates :columns, presence: true,
            numericality: { only_integer: true, greater_than: 0, less_than_or_equal_to: 6 }
  validates :user_id, uniqueness: true

  validate :max_groups_limit

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

  def has_groups?
    start_page_items.count > 0
  end

  private

  def max_groups_limit
    if start_page_groups.count > 10
      errors.add(:start_page_groups, "cannot exceed 10 groups")
    end
  end
end
