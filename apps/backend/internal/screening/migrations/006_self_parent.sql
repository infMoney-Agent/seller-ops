-- SellerSprite reports an independent listing with its own ASIN as parent.
ALTER TABLE ps_products DROP CONSTRAINT ps_products_check1;
ALTER TABLE ps_products ADD CONSTRAINT ps_products_parent_asin_format CHECK(parent_asin IS NULL OR parent_asin ~ '^[A-Z0-9]{10}$');
