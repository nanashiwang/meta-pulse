-- +goose Up
-- Catalog rows are upserted by slug. Observation tables are append-only and
-- deduplicated by a content fingerprint, so replaying an import is a no-op.

CREATE TABLE intel_provider (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  slug VARCHAR(80) NOT NULL,
  name VARCHAR(120) NOT NULL,
  website VARCHAR(500) NOT NULL DEFAULT '',
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_intel_provider_slug (slug)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE intel_model (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  slug VARCHAR(80) NOT NULL,
  name VARCHAR(120) NOT NULL,
  provider_slug VARCHAR(80) NOT NULL,
  family VARCHAR(60) NOT NULL DEFAULT '',
  modality VARCHAR(16) NOT NULL,
  status VARCHAR(16) NOT NULL,
  license VARCHAR(60) NOT NULL,
  open_weights TINYINT(1) NOT NULL DEFAULT 0,
  release_date DATE NULL,
  context_tokens BIGINT NOT NULL DEFAULT 0,
  max_output_tokens BIGINT NOT NULL DEFAULT 0,
  source_url VARCHAR(500) NOT NULL DEFAULT '',
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_intel_model_slug (slug),
  KEY idx_intel_model_provider (provider_slug),
  CONSTRAINT fk_intel_model_provider FOREIGN KEY (provider_slug) REFERENCES intel_provider (slug),
  CONSTRAINT ck_intel_model_tokens CHECK (context_tokens >= 0 AND max_output_tokens >= 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE intel_benchmark (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  slug VARCHAR(80) NOT NULL,
  name VARCHAR(120) NOT NULL,
  dimension VARCHAR(16) NOT NULL,
  unit VARCHAR(16) NOT NULL,
  higher_is_better TINYINT(1) NOT NULL,
  source_url VARCHAR(500) NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_intel_benchmark_slug (slug)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE intel_offering (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  slug VARCHAR(80) NOT NULL,
  provider_slug VARCHAR(80) NOT NULL,
  model_slug VARCHAR(80) NOT NULL,
  tier VARCHAR(40) NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_intel_offering_slug (slug),
  KEY idx_intel_offering_model (model_slug),
  CONSTRAINT fk_intel_offering_provider FOREIGN KEY (provider_slug) REFERENCES intel_provider (slug),
  CONSTRAINT fk_intel_offering_model FOREIGN KEY (model_slug) REFERENCES intel_model (slug)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE intel_import_run (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  source_slug VARCHAR(80) NOT NULL,
  note VARCHAR(500) NOT NULL DEFAULT '',
  catalog_rows INT NOT NULL,
  observations_added INT NOT NULL,
  observations_skipped INT NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  KEY idx_intel_import_run_created (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE intel_price_snapshot (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  offering_slug VARCHAR(80) NOT NULL,
  input_micro_usd BIGINT NOT NULL,
  output_micro_usd BIGINT NOT NULL,
  cache_read_micro_usd BIGINT NULL,
  effective_at DATETIME(3) NOT NULL,
  source_slug VARCHAR(80) NOT NULL,
  source_url VARCHAR(500) NOT NULL,
  fingerprint CHAR(64) NOT NULL,
  import_run_id BIGINT UNSIGNED NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_intel_price_fingerprint (fingerprint),
  KEY idx_intel_price_latest (offering_slug, effective_at, id),
  CONSTRAINT fk_intel_price_offering FOREIGN KEY (offering_slug) REFERENCES intel_offering (slug),
  CONSTRAINT fk_intel_price_run FOREIGN KEY (import_run_id) REFERENCES intel_import_run (id),
  CONSTRAINT ck_intel_price_nonneg CHECK (input_micro_usd >= 0 AND output_micro_usd >= 0 AND (cache_read_micro_usd IS NULL OR cache_read_micro_usd >= 0))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE intel_benchmark_result (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  model_slug VARCHAR(80) NOT NULL,
  benchmark_slug VARCHAR(80) NOT NULL,
  value BIGINT NOT NULL,
  settings VARCHAR(120) NOT NULL DEFAULT '',
  self_run TINYINT(1) NOT NULL DEFAULT 0,
  published_at DATETIME(3) NOT NULL,
  source_slug VARCHAR(80) NOT NULL,
  source_url VARCHAR(500) NOT NULL DEFAULT '',
  fingerprint CHAR(64) NOT NULL,
  import_run_id BIGINT UNSIGNED NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_intel_result_fingerprint (fingerprint),
  KEY idx_intel_result_latest (model_slug, benchmark_slug, published_at, id),
  CONSTRAINT fk_intel_result_model FOREIGN KEY (model_slug) REFERENCES intel_model (slug),
  CONSTRAINT fk_intel_result_benchmark FOREIGN KEY (benchmark_slug) REFERENCES intel_benchmark (slug),
  CONSTRAINT fk_intel_result_run FOREIGN KEY (import_run_id) REFERENCES intel_import_run (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE intel_speed_sample (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  offering_slug VARCHAR(80) NOT NULL,
  output_tokens_per_sec_milli BIGINT NOT NULL,
  ttft_ms BIGINT NOT NULL,
  measured_at DATETIME(3) NOT NULL,
  source_slug VARCHAR(80) NOT NULL,
  fingerprint CHAR(64) NOT NULL,
  import_run_id BIGINT UNSIGNED NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_intel_speed_fingerprint (fingerprint),
  KEY idx_intel_speed_window (offering_slug, measured_at),
  CONSTRAINT fk_intel_speed_offering FOREIGN KEY (offering_slug) REFERENCES intel_offering (slug),
  CONSTRAINT fk_intel_speed_run FOREIGN KEY (import_run_id) REFERENCES intel_import_run (id),
  CONSTRAINT ck_intel_speed_positive CHECK (output_tokens_per_sec_milli > 0 AND ttft_ms >= 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

-- One row per distinct board. The payload is the full materialised snapshot
-- so the home cards and table are always served from the same computation.
CREATE TABLE intel_score_batch (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  method_version VARCHAR(16) NOT NULL,
  fingerprint CHAR(64) NOT NULL,
  row_count INT NOT NULL,
  payload LONGTEXT NOT NULL,
  computed_at DATETIME(3) NOT NULL,
  UNIQUE KEY uk_intel_score_batch (method_version, fingerprint),
  KEY idx_intel_score_batch_latest (method_version, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

-- +goose Down
DROP TABLE intel_score_batch;
DROP TABLE intel_speed_sample;
DROP TABLE intel_benchmark_result;
DROP TABLE intel_price_snapshot;
DROP TABLE intel_import_run;
DROP TABLE intel_offering;
DROP TABLE intel_benchmark;
DROP TABLE intel_model;
DROP TABLE intel_provider;
