-- Seed data for Tusk development

-- Create some schemas
CREATE SCHEMA IF NOT EXISTS app;
CREATE SCHEMA IF NOT EXISTS analytics;

-- Create roles
CREATE ROLE readonly_user LOGIN PASSWORD 'readonly';
CREATE ROLE app_user LOGIN PASSWORD 'apppass' CREATEDB;
CREATE ROLE admin_user LOGIN PASSWORD 'adminpass' SUPERUSER;

-- App schema tables
CREATE TABLE app.users (
    id SERIAL PRIMARY KEY,
    email VARCHAR(255) UNIQUE NOT NULL,
    name VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE app.orders (
    id SERIAL PRIMARY KEY,
    user_id INT REFERENCES app.users(id),
    total_cents INT NOT NULL,
    status VARCHAR(50) DEFAULT 'pending',
    created_at TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE app.products (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    price_cents INT NOT NULL,
    category VARCHAR(100),
    inventory INT DEFAULT 0,
    created_at TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE app.order_items (
    id SERIAL PRIMARY KEY,
    order_id INT REFERENCES app.orders(id),
    product_id INT REFERENCES app.products(id),
    quantity INT NOT NULL,
    price_cents INT NOT NULL
);

CREATE TABLE app.sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id INT REFERENCES app.users(id),
    token TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ DEFAULT now()
);

-- Analytics schema tables
CREATE TABLE analytics.events (
    id BIGSERIAL PRIMARY KEY,
    event_type VARCHAR(100) NOT NULL,
    user_id INT,
    payload JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE analytics.page_views (
    id BIGSERIAL PRIMARY KEY,
    path VARCHAR(500) NOT NULL,
    user_id INT,
    referrer VARCHAR(500),
    duration_ms INT,
    created_at TIMESTAMPTZ DEFAULT now()
);

-- Indexes
CREATE INDEX idx_orders_user_id ON app.orders(user_id);
CREATE INDEX idx_orders_status ON app.orders(status);
CREATE INDEX idx_order_items_order_id ON app.order_items(order_id);
CREATE INDEX idx_order_items_product_id ON app.order_items(product_id);
CREATE INDEX idx_products_category ON app.products(category);
CREATE INDEX idx_events_type ON analytics.events(event_type);
CREATE INDEX idx_events_created ON analytics.events(created_at);
CREATE INDEX idx_page_views_path ON analytics.page_views(path);

-- Seed users
INSERT INTO app.users (email, name) VALUES
    ('alice@example.com', 'Alice Johnson'),
    ('bob@example.com', 'Bob Smith'),
    ('charlie@example.com', 'Charlie Brown'),
    ('diana@example.com', 'Diana Prince'),
    ('eve@example.com', 'Eve Wilson');

-- Seed products
INSERT INTO app.products (name, price_cents, category, inventory) VALUES
    ('Widget A', 1999, 'widgets', 150),
    ('Widget B', 2999, 'widgets', 75),
    ('Gadget X', 4999, 'gadgets', 200),
    ('Gadget Y', 7999, 'gadgets', 30),
    ('Doohickey', 999, 'misc', 500);

-- Seed orders
INSERT INTO app.orders (user_id, total_cents, status) VALUES
    (1, 4998, 'completed'),
    (1, 7999, 'shipped'),
    (2, 2999, 'pending'),
    (3, 1998, 'completed'),
    (4, 4999, 'processing'),
    (5, 999, 'pending'),
    (2, 8998, 'completed'),
    (3, 4999, 'shipped');

-- Seed order items
INSERT INTO app.order_items (order_id, product_id, quantity, price_cents) VALUES
    (1, 1, 2, 1999),
    (2, 4, 1, 7999),
    (3, 2, 1, 2999),
    (4, 1, 1, 1999),
    (5, 3, 1, 4999),
    (6, 5, 1, 999),
    (7, 2, 1, 2999),
    (7, 3, 1, 4999),
    (8, 3, 1, 4999);

-- Seed analytics events
INSERT INTO analytics.events (event_type, user_id, payload)
SELECT
    (ARRAY['page_view', 'click', 'purchase', 'signup', 'logout'])[1 + (random()*4)::int],
    1 + (random()*4)::int,
    jsonb_build_object('source', (ARRAY['web', 'mobile', 'api'])[1 + (random()*2)::int])
FROM generate_series(1, 10000);

-- Seed page views
INSERT INTO analytics.page_views (path, user_id, referrer, duration_ms)
SELECT
    (ARRAY['/', '/products', '/cart', '/checkout', '/account', '/search', '/about'])[1 + (random()*6)::int],
    1 + (random()*4)::int,
    (ARRAY['https://google.com', 'https://twitter.com', NULL, NULL])[1 + (random()*3)::int],
    (random() * 30000)::int
FROM generate_series(1, 50000);

-- Grant privileges
GRANT USAGE ON SCHEMA app TO readonly_user;
GRANT SELECT ON ALL TABLES IN SCHEMA app TO readonly_user;
GRANT USAGE ON SCHEMA analytics TO readonly_user;
GRANT SELECT ON ALL TABLES IN SCHEMA analytics TO readonly_user;

GRANT ALL ON SCHEMA app TO app_user;
GRANT ALL ON ALL TABLES IN SCHEMA app TO app_user;
GRANT ALL ON ALL SEQUENCES IN SCHEMA app TO app_user;
