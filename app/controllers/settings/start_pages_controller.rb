class Settings::StartPagesController < ApplicationController
  def show
    @start_page = current_user.start_page || current_user.build_start_page(name: "Start", columns: 3)
  end
end
