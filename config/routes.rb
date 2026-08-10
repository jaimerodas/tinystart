Rails.application.routes.draw do
  resource :session

  # No :show — the start page is served at "/" and nowhere else. /start survives
  # as the PATCH target for the column count and as the prefix every group and
  # item route hangs off; a GET there is a 404 on purpose.
  resource :start, controller: :start_pages, only: [ :edit, :update ] do
    resources :groups, controller: :start_page_groups, only: [ :create, :update, :destroy ] do
      member { post :move }
    end
    resources :items, controller: :start_page_items, only: [ :create, :update, :destroy ] do
      member do
        post :move
        post :visit
      end
    end
  end
  resource :search, only: [ :show ], controller: "search"
  resources :visits, only: [ :create ]
  resources :passwords, param: :token
  resource :settings, only: [ :show, :update ]

  namespace :settings do
    resource :password, only: [ :edit, :update ]
    # Singular resource — there is one connection per user — but named in the
    # plural, because that is what the section is called and what the URL
    # should read.
    resource :connections, only: [ :show, :create, :destroy ],
             controller: "connections" do
      get :poll
    end

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

  root "start_pages#show"
end
