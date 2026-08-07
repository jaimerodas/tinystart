class StartPageGroupsController < ApplicationController
  layout "start"

  before_action :set_start_page
  before_action :set_group, only: [ :update, :destroy, :move ]

  # POST /start/groups
  def create
    @group = @start_page.start_page_groups.build(group_params)

    if @group.save
      redirect_to edit_start_path, notice: "Group created successfully."
    else
      redirect_to edit_start_path, alert: "Failed to create group: #{@group.errors.full_messages.join(', ')}"
    end
  end

  # PATCH/PUT /start/groups/1
  def update
    if @group.update(group_params)
      redirect_to edit_start_path, notice: "Group updated successfully."
    else
      redirect_to edit_start_path, alert: "Failed to update group: #{@group.errors.full_messages.join(', ')}"
    end
  end

  # DELETE /start/groups/1
  def destroy
    @group.destroy
    redirect_to edit_start_path, notice: "Group deleted successfully."
  end

  # POST /start/groups/1/move
  def move
    column = params[:column].to_i
    position = params[:position].to_i

    if @group.move_to_column(column, position)
      respond_to do |format|
        format.html { redirect_to edit_start_path, notice: "Group moved successfully." }
        format.json { render json: { status: "success", message: "Group moved successfully." } }
        format.turbo_stream { render turbo_stream: turbo_stream.replace("start_page_grid", partial: "start_pages/grid", locals: { start_page: @start_page, groups_by_column: @start_page.groups_by_column }) }
      end
    else
      respond_to do |format|
        format.html { redirect_to edit_start_path, alert: "Failed to move group." }
        format.json { render json: { status: "error", message: "Failed to move group." }, status: 422 }
        format.turbo_stream { render turbo_stream: turbo_stream.replace("error_messages", partial: "shared/error_message", locals: { message: "Failed to move group." }) }
      end
    end
  end

  private

  def set_start_page
    @start_page = current_user.start_page
    redirect_to settings_start_page_path unless @start_page
  end

  def set_group
    @group = @start_page.start_page_groups.find(params[:id])
  end

  def group_params
    params.require(:start_page_group).permit(:name, :column, :position)
  end
end
