class VisitsController < ApplicationController
  # POST /visits
  #
  # Federated results belong to tinylinks, so their visits are forwarded there.
  # Local tiles are counted by StartPageItemsController#visit instead.
  def create
    TinylinksClient.new(current_user.tinylinks_connection).record_visit(params[:link_id])
    head :no_content
  end
end
