CREATE TABLE IF NOT EXISTS orders (id INTEGER PRIMARY KEY, customer TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS items (id INTEGER PRIMARY KEY, order_id INTEGER NOT NULL, sku TEXT NOT NULL, cents INTEGER NOT NULL);
DELETE FROM items;
DELETE FROM orders;
INSERT INTO orders (id, customer) VALUES (1, 'mock-customer-1'), (2, 'mock-customer-2');
INSERT INTO items (order_id, sku, cents) VALUES
  (1, 'MOCK-SKU-A', 25),
  (1, 'MOCK-SKU-B', 300),
  (1, 'MOCK-SKU-C', 350),
  (2, 'MOCK-SKU-A', 25);
