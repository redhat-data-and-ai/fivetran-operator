-- E2E test schema for Fivetran operator schema policy tests.
-- 3 schemas, 7 tables with primary keys and foreign keys.

CREATE SCHEMA IF NOT EXISTS e2e_public;
CREATE SCHEMA IF NOT EXISTS e2e_inventory;
CREATE SCHEMA IF NOT EXISTS e2e_analytics;

-- e2e_public: users, orders, logs
CREATE TABLE IF NOT EXISTS e2e_public.users (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    email VARCHAR(100) NOT NULL,
    password VARCHAR(100) NOT NULL,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS e2e_public.orders (
    id SERIAL PRIMARY KEY,
    user_id INTEGER REFERENCES e2e_public.users(id),
    total DECIMAL(10,2) NOT NULL,
    status VARCHAR(20) NOT NULL,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS e2e_public.logs (
    id SERIAL PRIMARY KEY,
    message TEXT NOT NULL,
    level VARCHAR(10) NOT NULL,
    created_at TIMESTAMP DEFAULT NOW()
);

-- e2e_inventory: products, warehouses
CREATE TABLE IF NOT EXISTS e2e_inventory.products (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    price DECIMAL(10,2) NOT NULL,
    category VARCHAR(50) NOT NULL
);

CREATE TABLE IF NOT EXISTS e2e_inventory.warehouses (
    id SERIAL PRIMARY KEY,
    location VARCHAR(100) NOT NULL,
    capacity INTEGER NOT NULL
);

-- e2e_analytics: page_views, sessions
CREATE TABLE IF NOT EXISTS e2e_analytics.page_views (
    id SERIAL PRIMARY KEY,
    url VARCHAR(255) NOT NULL,
    user_id INTEGER NOT NULL,
    viewed_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS e2e_analytics.sessions (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL,
    duration INTEGER NOT NULL,
    start_time TIMESTAMP DEFAULT NOW()
);

-- Seed minimal test data
INSERT INTO e2e_public.users (name, email, password)
VALUES ('test_user_1', 'test1@example.com', 'placeholder'),
       ('test_user_2', 'test2@example.com', 'placeholder')
ON CONFLICT DO NOTHING;

INSERT INTO e2e_public.orders (user_id, total, status)
VALUES (1, 99.99, 'completed'),
       (2, 49.50, 'pending')
ON CONFLICT DO NOTHING;

INSERT INTO e2e_public.logs (message, level)
VALUES ('test log entry', 'INFO')
ON CONFLICT DO NOTHING;

INSERT INTO e2e_inventory.products (name, price, category)
VALUES ('Widget A', 19.99, 'gadgets'),
       ('Widget B', 29.99, 'gadgets')
ON CONFLICT DO NOTHING;

INSERT INTO e2e_inventory.warehouses (location, capacity)
VALUES ('US-West', 1000)
ON CONFLICT DO NOTHING;

INSERT INTO e2e_analytics.page_views (url, user_id)
VALUES ('/home', 1),
       ('/products', 2)
ON CONFLICT DO NOTHING;

INSERT INTO e2e_analytics.sessions (user_id, duration)
VALUES (1, 300),
       (2, 120)
ON CONFLICT DO NOTHING;

-- Create a read-only user for Fivetran
DROP ROLE IF EXISTS fivetran_e2e;
CREATE ROLE fivetran_e2e WITH LOGIN PASSWORD :'fivetran_password';

GRANT USAGE ON SCHEMA e2e_public TO fivetran_e2e;
GRANT USAGE ON SCHEMA e2e_inventory TO fivetran_e2e;
GRANT USAGE ON SCHEMA e2e_analytics TO fivetran_e2e;
GRANT SELECT ON ALL TABLES IN SCHEMA e2e_public TO fivetran_e2e;
GRANT SELECT ON ALL TABLES IN SCHEMA e2e_inventory TO fivetran_e2e;
GRANT SELECT ON ALL TABLES IN SCHEMA e2e_analytics TO fivetran_e2e;
