-- Add index for payment_created_at to optimize dashboard real-time funnel chart queries
CREATE INDEX IF NOT EXISTS idx_payments_created_at 
ON payments (payment_created_at) 
WHERE payment_is_deleted = FALSE;
