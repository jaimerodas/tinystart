class StartPageItem < ApplicationRecord
  belongs_to :start_page_group

  validates :url, presence: true, uniqueness: { scope: :start_page_group_id }
  validates :title, presence: true
  validates :position, presence: true,
            numericality: { only_integer: true, greater_than_or_equal_to: 0 }

  validate :valid_url

  scope :ordered, -> { order(:position) }

  def move_to_group(new_group, new_position = nil)
    return false unless new_group.is_a?(StartPageGroup)

    self.start_page_group = new_group
    self.position = new_position || new_group.start_page_items.maximum(:position).to_i + 1
    save
  end

  def move_to_position(new_position)
    return false if new_position < 0

    update(position: new_position)
  end

  def reorder_group_positions!
    start_page_group.reorder_positions!
  end

  private

  def valid_url
    return if url.blank?

    uri = URI.parse(url)
    unless uri.is_a?(URI::HTTP) || uri.is_a?(URI::HTTPS)
      errors.add(:url, "must be a valid URL")
    end
  rescue URI::InvalidURIError
    errors.add(:url, "must be a valid URL")
  end
end
