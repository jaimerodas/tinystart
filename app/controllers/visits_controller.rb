class VisitsController < ApplicationController
  # POST /visits
  #
  # Federated results belong to the connected app, so their visits are
  # forwarded there.
  # Local tiles are counted by StartPageItemsController#visit instead.
  def create
    ConnectionClient.new(current_user.connection).record_visit(params[:link_id])
    head :no_content
  end
end
