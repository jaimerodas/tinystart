class StartPagesController < ApplicationController
  layout "start"

  # GET /
  def show
    @groups_by_column = current_user.groups_by_column
    @links_json = current_user.links_for_command_bar.to_json
    @tinylinks_connection = current_user.tinylinks_connection
  end

  # GET /start/edit
  def edit
    @groups_by_column = current_user.groups_by_column
  end
end
