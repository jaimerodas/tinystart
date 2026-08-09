class StartPagesController < ApplicationController
  include StartPageNotice

  layout "start"

  # GET /
  def show
    links = current_user.links_for_command_bar
    @groups_by_column = current_user.groups_by_column
    @links_json = links.to_json
    @has_tiles = links.any?
    @connection = current_user.connection
  end

  # GET /start/edit
  def edit
    @groups_by_column = current_user.groups_by_column
  end

  # PATCH /start
  # The column count, and nothing else — the rest of the user's preferences are
  # SettingsController's.
  def update
    if current_user.update(columns_param)
      # A full visit rather than a stream: every column moves, and redrawing
      # them one by one would mean replacing #start_page_grid, the node that
      # carries the drag and keyboard controllers.
      redirect_to edit_start_path
    else
      # The select is already showing the value the database refused, so
      # reporting is not enough — it has to be sent back as well.
      message = current_user.errors.full_messages.to_sentence

      # reload restores the stored value but leaves the errors on the object,
      # and Rails wraps every field of a model that has any in a
      # .field_with_errors div — which would break the toolbar row apart. The
      # refusal is spoken by the notice; the select just goes back to the truth.
      current_user.reload
      current_user.errors.clear

      respond_to do |format|
        # Without a stream to apply, the message has nowhere to land but a
        # flash — same refusal, said the only way the page can hear it.
        format.html { redirect_to edit_start_path, alert: message }
        format.turbo_stream do
          render turbo_stream: [
                   notice_stream(message),
                   turbo_stream.replace(helpers.column_count_dom_id,
                                        partial: "start_pages/column_count",
                                        locals: { user: current_user })
                 ],
                 status: :unprocessable_content
        end
      end
    end
  end

  private

  def columns_param
    params.require(:user).permit(:columns)
  end
end
