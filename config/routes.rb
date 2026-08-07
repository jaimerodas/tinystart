Rails.application.routes.draw do
  resource :session

  resource :start, controller: :start_pages, only: [ :show, :create, :edit, :update ] do
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
    resource :start_page, only: [ :show ], path: "start-page"
    resource :tinylinks, only: [ :show, :create, :destroy ],
             controller: "tinylinks_connections" do
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
