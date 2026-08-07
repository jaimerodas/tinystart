class StartPageItemsController < ApplicationController
  before_action :set_item, only: [ :update, :destroy, :move, :visit ]

  layout "start"

  # POST /start/items
  def create
    @group = current_user.start_page_groups.find(params[:group_id])

    @item = @group.start_page_items.build(
      item_params.merge(position: @group.start_page_items.count)
    )

    if @item.save
      redirect_to edit_start_path, notice: "Tile added."
    else
      redirect_to edit_start_path, alert: "Failed to add tile: #{@item.errors.full_messages.join(', ')}"
    end
  end

  # PATCH /start/items/1
  # There is no metadata to re-fetch here, so a typo'd title has to be fixable.
  def update
    if @item.update(item_params)
      redirect_to edit_start_path, notice: "Tile updated."
    else
      redirect_to edit_start_path, alert: "Failed to update tile: #{@item.errors.full_messages.join(', ')}"
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
    redirect_to edit_start_path, notice: "Tile removed."
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

    if group_id.present?
      # Moving to a different group
      new_group = current_user.start_page_groups.find(group_id)
      success = @item.move_to_group(new_group, new_position)
      @item.reorder_group_positions! if success
    else
      # Moving within the same group
      success = @item.start_page_group.move_item_to_position(@item, new_position)
    end

    if success
      respond_to do |format|
        format.html { redirect_to edit_start_path, notice: "Item moved successfully." }
        format.json { render json: { status: "success", message: "Item moved successfully." } }
        format.turbo_stream { render turbo_stream: turbo_stream.replace("start_page_grid", partial: "start_pages/grid", locals: { user: current_user, groups_by_column: current_user.groups_by_column }) }
      end
    else
      respond_to do |format|
        format.html { redirect_to edit_start_path, alert: "Failed to move item." }
        format.json { render json: { status: "error", message: "Failed to move item." }, status: 422 }
        format.turbo_stream { render turbo_stream: turbo_stream.replace("error_messages", partial: "shared/error_message", locals: { message: "Failed to move item." }) }
      end
    end
  end

  private

  def set_item
    @item = current_user.start_page_items.find(params[:id])
  end

  def item_params
    params.require(:start_page_item).permit(:url, :title)
  end
end
