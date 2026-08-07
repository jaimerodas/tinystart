Rails.application.routes.draw do
  resource :session
  resources :passwords, param: :token
  resource :settings, only: [ :show, :update ]

  namespace :settings do
    resource :password, only: [ :edit, :update ]

    scope :admin do
      resources :users, only: [ :index ] do
        post "approve", to: "admin_user_actions#approve", as: "approve"
        post "password_reset", to: "admin_user_actions#password_reset", as: "password_reset"
      end
    end
  end

  get "sign_up", to: "users#new", as: "sign_up"
  post "sign_up", to: "users#create", as: "create_user"

  # Reveal health status on /up that returns 200 if the app boots with no exceptions, otherwise 500.
  get "up" => "rails/health#show", as: :rails_health_check

  # The start page itself lands here in phase 4. Until then, settings is the
  # only thing to look at.
  root "settings#show"
end
