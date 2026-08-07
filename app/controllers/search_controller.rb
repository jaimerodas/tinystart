class SearchController < ApplicationController
  # GET /search.json?q=
  #
  # Proxies the command bar's federated query to tinylinks server-side, so the
  # token never reaches the browser and there's no CORS to configure. Shape is
  # the bare array the command bar already expects.
  def show
    render json: TinylinksClient.new(current_user.tinylinks_connection).search(params[:q])
  end
end
