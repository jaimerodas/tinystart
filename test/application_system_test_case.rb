require "test_helper"

class ApplicationSystemTestCase < ActionDispatch::SystemTestCase
  driven_by :selenium, using: :headless_chrome, screen_size: [ 1400, 1400 ]

  # The compact forms on the edit page label their fields with aria-label
  # rather than a visible <label>, and that is the field's accessible name —
  # so a test should be able to find it the same way a screen reader does.
  Capybara.enable_aria_label = true
end
