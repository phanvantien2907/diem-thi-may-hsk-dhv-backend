-- Adding indexes for foreign keys queried by the location module
-- specifically for StatsCandidatesByProvince and MergeWard operations

CREATE INDEX IF NOT EXISTS idx_candidate_documents_ward_id
    ON candidate_documents (candidate_document_ward_id)
    WHERE candidate_document_is_deleted = FALSE;

CREATE INDEX IF NOT EXISTS idx_certificate_deliveries_ward_id
    ON certificate_deliveries (certificate_delivery_ward_id)
    WHERE certificate_delivery_is_deleted = FALSE;
