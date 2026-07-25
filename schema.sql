-- 親テーブル
CREATE TABLE orders (
  id VARCHAR(36) PRIMARY KEY,
  customer_name VARCHAR(100),
  delivery_address VARCHAR(255),
  created_at TIMESTAMPTZ
);

-- 子テーブル
CREATE TABLE order_items (
  id VARCHAR(36),
  item_id VARCHAR(36),
  item_name VARCHAR(100),
  quantity BIGINT,
  notes VARCHAR(255),
  PRIMARY KEY (id, item_id)
) INTERLEAVE IN PARENT orders ON DELETE CASCADE;
