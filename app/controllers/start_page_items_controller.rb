class StartPageItemsController < ApplicationController
  include StartPageNotice

  before_action :set_item, only: [ :update, :destroy, :move, :visit ]

  layout "start"

  # POST /start/items
  def create
    @group = current_user.start_page_groups.find(params[:group_id])

    # Position comes from the group, not the form — see place_at_end_of_group.
    @item = @group.start_page_items.build(item_params)

    if @item.save
      respond_to do |format|
        format.html { redirect_to edit_start_path, notice: "Tile added." }
        # Left open so the next link can be typed straight away
        format.turbo_stream { render turbo_stream: group_stream(@group, open_form: :add_item) }
      end
    else
      respond_to do |format|
        format.html { redirect_to edit_start_path, alert: "Failed to add tile: #{@item.errors.full_messages.join(', ')}" }
        format.turbo_stream { render turbo_stream: new_item_stream(@item, @group), status: :unprocessable_content }
      end
    end
  end

  # PATCH /start/items/1
  # There is no metadata to re-fetch here, so a typo'd title has to be fixable.
  def update
    if @item.update(item_params)
      respond_to do |format|
        format.html { redirect_to edit_start_path, notice: "Tile updated." }
        format.turbo_stream { render turbo_stream: item_stream(@item) }
      end
    else
      respond_to do |format|
        format.html { redirect_to edit_start_path, alert: "Failed to update tile: #{@item.errors.full_messages.join(', ')}" }
        format.turbo_stream do
          render turbo_stream: item_stream(@item, open_form: :edit_item), status: :unprocessable_content
        end
      end
    end
  end

  # DELETE /start/items/1
  def destroy
    group = @item.start_page_group
    # Removing the item and closing the gap it leaves has to happen as one unit
    ActiveRecord::Base.transaction do
      @item.destroy
      group.reorder_positions!
    end

    respond_to do |format|
      format.html { redirect_to edit_start_path, notice: "Tile removed." }
      format.turbo_stream { render turbo_stream: group_stream(group.reload) }
    end
  end

  # POST /start/items/1/visit
  # Fire-and-forget from the grid: bump the counter without touching
  # validations, and say nothing back.
  def visit
    StartPageItem.where(id: @item.id).update_all("visit_count = visit_count + 1")
    head :no_content
  end

  # POST /start/items/1/move
  def move
    group_id = params[:group_id]
    new_position = params[:position].to_i
    source_group = @item.start_page_group

    if group_id.present?
      # Moving to a different group. move_to_group renumbers both groups itself.
      new_group = current_user.start_page_groups.find(group_id)
      success = @item.move_to_group(new_group, new_position)
    else
      # Moving within the same group
      new_group = source_group
      success = source_group.move_item_to_position(@item, new_position)
    end

    if success
      respond_to do |format|
        format.html { redirect_to edit_start_path, notice: "Item moved successfully." }
        format.json { render json: { status: "success", message: "Item moved successfully." } }
        format.turbo_stream { render turbo_stream: move_streams(source_group, new_group) }
      end
    else
      respond_to do |format|
        format.html { redirect_to edit_start_path, alert: "Failed to move item." }
        format.json { render json: { status: "error", message: "Failed to move item." }, status: 422 }
        format.turbo_stream do
          # The client moved the tile before it asked, so saying no is not
          # enough — the groups have to be redrawn from what is actually stored,
          # or the page keeps an order the database never accepted and the next
          # move takes its index from it.
          render turbo_stream: [ notice_stream("Failed to move item.") ] + move_streams(source_group, new_group),
                 status: :unprocessable_content
        end
      end
    end
  end

  private

  # A move renumbers the group the tile left and the group it landed in, and
  # nothing else. Redrawing the whole grid instead would replace
  # #start_page_grid, the node carrying the drag and keyboard controllers, and
  # take the keyboard highlight with it on every move.
  def move_streams(source_group, destination_group)
    streams = [ group_stream(destination_group.reload) ]
    streams << group_stream(source_group.reload) if source_group != destination_group
    streams
  end

  # A group owns the tile rows and their positions, so anything that adds,
  # removes or reorders a tile redraws the group rather than the tile.
  def group_stream(group, open_form: nil)
    turbo_stream.replace(
      helpers.group_dom_id(group),
      partial: "start_pages/group",
      locals: { group: group, column_number: group.column, open_form: open_form }
    )
  end

  def item_stream(item, open_form: nil)
    turbo_stream.replace(
      helpers.item_dom_id(item),
      partial: "start_pages/item",
      locals: { item: item, group: item.start_page_group, open_form: open_form }
    )
  end

  def new_item_stream(item, group)
    turbo_stream.replace(
      helpers.new_item_dom_id(group),
      partial: "start_page_items/new",
      locals: { item: item, group: group, open: true }
    )
  end

  def set_item
    @item = current_user.start_page_items.find(params[:id])
  end

  def item_params
    params.require(:start_page_item).permit(:url, :title)
  end
end
