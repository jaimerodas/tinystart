class StartPageGroupsController < ApplicationController
  include StartPageNotice

  layout "start"

  before_action :set_group, only: [ :update, :destroy, :move ]

  # POST /start/groups
  # Position comes from the column, not the form — see StartPageGroup's
  # place_at_end_of_column.
  def create
    @group = current_user.start_page_groups.build(group_params)

    if @group.save
      respond_to do |format|
        format.html { redirect_to edit_start_path, notice: "Group created successfully." }
        format.turbo_stream { render turbo_stream: column_stream(@group.column) }
      end
    else
      respond_to do |format|
        format.html { redirect_to edit_start_path, alert: "Failed to create group: #{@group.errors.full_messages.join(', ')}" }
        format.turbo_stream { render turbo_stream: new_group_stream(@group), status: :unprocessable_content }
      end
    end
  end

  # PATCH/PUT /start/groups/1
  # A rename touches no sibling, so only the group itself has to be redrawn.
  def update
    if @group.update(group_params)
      respond_to do |format|
        format.html { redirect_to edit_start_path, notice: "Group updated successfully." }
        format.turbo_stream { render turbo_stream: group_stream(@group) }
      end
    else
      respond_to do |format|
        format.html { redirect_to edit_start_path, alert: "Failed to update group: #{@group.errors.full_messages.join(', ')}" }
        format.turbo_stream do
          render turbo_stream: group_stream(@group, open_form: :edit_group), status: :unprocessable_content
        end
      end
    end
  end

  # DELETE /start/groups/1
  def destroy
    column = @group.column
    # Removing the group and closing the gap it leaves has to happen as one unit
    ActiveRecord::Base.transaction do
      @group.destroy
      current_user.reorder_groups_in_column!(column)
    end

    respond_to do |format|
      format.html { redirect_to edit_start_path, notice: "Group deleted successfully." }
      format.turbo_stream { render turbo_stream: column_stream(column) }
    end
  end

  # POST /start/groups/1/move
  def move
    column = params[:column].to_i
    position = params[:position].to_i
    source_column = @group.column

    if @group.move_to_column(column, position)
      respond_to do |format|
        format.html { redirect_to edit_start_path, notice: "Group moved successfully." }
        format.json { render json: { status: "success", message: "Group moved successfully." } }
        format.turbo_stream { render turbo_stream: move_streams(source_column, column) }
      end
    else
      respond_to do |format|
        format.html { redirect_to edit_start_path, alert: "Failed to move group." }
        format.json { render json: { status: "error", message: "Failed to move group." }, status: 422 }
        format.turbo_stream do
          # The client moved the group before it asked, so saying no is not
          # enough — the columns have to be redrawn from what is actually
          # stored, or the page keeps a placement the database never accepted
          # and the next move takes its index from it.
          render turbo_stream: [ notice_stream("Failed to move group.") ] + resync_streams(source_column, column),
                 status: :unprocessable_content
        end
      end
    end
  end

  private

  # A move renumbers the column the group left and the column it landed in, and
  # nothing else. Redrawing the whole grid instead would replace
  # #start_page_grid, the node carrying the drag and keyboard controllers, and
  # take the keyboard highlight with it on every move.
  def move_streams(source_column, destination_column)
    streams = [ column_stream(destination_column) ]
    streams << column_stream(source_column) if source_column != destination_column
    streams
  end

  # The same two columns, but only the ones that exist. A move is refused
  # precisely when the column asked for is off the end of the grid, and there is
  # no node on the page to redraw for a column the user does not have.
  def resync_streams(source_column, destination_column)
    ([ source_column, destination_column ].uniq & current_user.column_range).map { |number| column_stream(number) }
  end

  def column_stream(column_number)
    turbo_stream.replace(
      helpers.column_dom_id(column_number),
      partial: "start_pages/column",
      locals: {
        column_number: column_number,
        groups: current_user.groups_in_column(column_number).to_a
      }
    )
  end

  def group_stream(group, open_form: nil)
    turbo_stream.replace(
      helpers.group_dom_id(group),
      partial: "start_pages/group",
      locals: { group: group, column_number: group.column, open_form: open_form }
    )
  end

  def new_group_stream(group)
    # A blank column would address a slot that does not exist, and Turbo applies
    # a stream with no target to nothing at all — the error would vanish.
    column_number = group.column.presence || current_user.column_range.first

    turbo_stream.replace(
      helpers.new_group_dom_id(column_number),
      partial: "start_page_groups/new",
      locals: { group: group, column_number: column_number, open: true }
    )
  end

  def set_group
    @group = current_user.start_page_groups.find(params[:id])
  end

  def group_params
    params.require(:start_page_group).permit(:name, :column)
  end
end
