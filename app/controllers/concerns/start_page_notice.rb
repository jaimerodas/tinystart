# The one place a failed move can say so. The edit page renders the region empty
# and the failing action fills it, because a stream aimed at an id that is
# nowhere on the page is applied to nothing at all — which is how move failures
# used to disappear.
module StartPageNotice
  extend ActiveSupport::Concern

  private

  # update, not replace: the region is a live one, and it is only announced for
  # changes made inside it while it is already in the accessibility tree.
  # Replacing it would hand the reader a region that already had its text, which
  # is the shape screen readers stay quiet about.
  def notice_stream(message)
    turbo_stream.update("start_page_notice", partial: "shared/error_message", locals: { message: message })
  end
end
