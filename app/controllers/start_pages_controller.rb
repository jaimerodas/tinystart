class StartPagesController < ApplicationController
  before_action :set_start_page, only: [ :show, :edit, :update ]
  before_action :ensure_start_page, only: [ :show ]

  layout "start"

  # GET /
  def show
    @groups_by_column = @start_page.groups_by_column
    @links_json = @start_page.links_for_command_bar.to_json
  end

  # GET /start/edit
  def edit
    @groups_by_column = @start_page.groups_by_column
  end

  # POST /start
  def create
    @start_page = current_user.build_start_page(start_page_params)

    if @start_page.save
      redirect_to settings_start_page_path, notice: "Start page created successfully."
    else
      redirect_to settings_start_page_path, alert: "Failed to create start page: #{@start_page.errors.full_messages.join(', ')}"
    end
  end

  # PATCH/PUT /start
  def update
    if @start_page.update(start_page_params)
      redirect_to settings_start_page_path, notice: "Start page updated successfully."
    else
      render :edit
    end
  end

  private

  def set_start_page
    @start_page = current_user.start_page
  end

  def ensure_start_page
    unless @start_page
      redirect_to settings_start_page_path, notice: "Create your start page to get started."
    end
  end

  def start_page_params
    params.require(:start_page).permit(:name, :columns)
  end
end
