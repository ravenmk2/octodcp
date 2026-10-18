-- OctoDCP Phase A: MySQL 8+ (minimum 8.0.16) / InnoDB reference DDL.
-- Design draft only: convert into versioned goose migrations before implementation.
-- Run against an empty, dedicated schema. No default accounts, seed data or secrets.
-- MySQL DDL implicitly commits; this file is not an atomic installation transaction.
-- Application timestamps are UTC. Configure each connection with time_zone = '+00:00'.

CREATE TABLE `user` (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    username VARCHAR(30) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    display_name VARCHAR(64) NOT NULL,
    role VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'user',
    status VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'active',
    avatar_file_id BIGINT UNSIGNED NULL,
    auth_version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    row_version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    last_login_at DATETIME(3) NULL,
    created_at DATETIME(3) NOT NULL,
    updated_at DATETIME(3) NOT NULL,
    deleted_at DATETIME(3) NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uq_user_username (username),
    UNIQUE KEY uq_user_avatar_file_id (avatar_file_id),
    KEY idx_user_deleted_created (deleted_at, created_at, id),
    KEY idx_user_deleted_status (deleted_at, status, created_at, id),
    KEY idx_user_deleted_role (deleted_at, role, created_at, id),
    CONSTRAINT ck_user_username_length CHECK (CHAR_LENGTH(username) BETWEEN 3 AND 30),
    CONSTRAINT ck_user_display_name_length CHECK (CHAR_LENGTH(display_name) BETWEEN 1 AND 30),
    CONSTRAINT ck_user_role CHECK (role IN ('admin', 'user')),
    CONSTRAINT ck_user_status CHECK (status IN ('active', 'disabled')),
    CONSTRAINT ck_user_deleted_status CHECK (deleted_at IS NULL OR status = 'disabled'),
    CONSTRAINT ck_user_versions CHECK (auth_version >= 1 AND row_version >= 1)
) ENGINE=InnoDB DEFAULT CHARACTER SET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `user_password` (
    user_id BIGINT UNSIGNED NOT NULL,
    password_hash VARCHAR(255) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    require_password_change BOOLEAN NOT NULL DEFAULT FALSE,
    password_changed_at DATETIME(3) NOT NULL,
    PRIMARY KEY (user_id),
    CONSTRAINT fk_user_password_user FOREIGN KEY (user_id) REFERENCES `user` (id) ON DELETE CASCADE,
    CONSTRAINT ck_user_password_require_change CHECK (require_password_change IN (0, 1))
) ENGINE=InnoDB DEFAULT CHARACTER SET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `avatar_file` (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    storage_key VARCHAR(120) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    content_type VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    width SMALLINT UNSIGNED NOT NULL,
    height SMALLINT UNSIGNED NOT NULL,
    size_bytes INT UNSIGNED NOT NULL,
    sha256 BINARY(32) NOT NULL,
    uploaded_by_user_id BIGINT UNSIGNED NOT NULL,
    state VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'staged',
    expires_at DATETIME(3) NULL,
    orphaned_at DATETIME(3) NULL,
    created_at DATETIME(3) NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uq_avatar_file_storage_key (storage_key),
    KEY idx_avatar_file_staged_expiry (state, expires_at, id),
    KEY idx_avatar_file_orphaned_time (state, orphaned_at, id),
    KEY idx_avatar_file_uploader (uploaded_by_user_id, created_at, id),
    CONSTRAINT fk_avatar_file_uploader FOREIGN KEY (uploaded_by_user_id) REFERENCES `user` (id) ON DELETE RESTRICT,
    CONSTRAINT ck_avatar_file_type CHECK (content_type = 'image/png'),
    CONSTRAINT ck_avatar_file_dimensions CHECK (width = 256 AND height = 256),
    CONSTRAINT ck_avatar_file_size CHECK (size_bytes BETWEEN 1 AND 524288),
    CONSTRAINT ck_avatar_file_state CHECK (state IN ('staged', 'attached', 'orphaned')),
    CONSTRAINT ck_avatar_file_lifecycle CHECK (
        (state = 'staged' AND expires_at IS NOT NULL AND orphaned_at IS NULL)
        OR (state = 'attached' AND expires_at IS NULL AND orphaned_at IS NULL)
        OR (state = 'orphaned' AND expires_at IS NULL AND orphaned_at IS NOT NULL)
    )
) ENGINE=InnoDB DEFAULT CHARACTER SET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

ALTER TABLE `user`
    ADD CONSTRAINT fk_user_avatar FOREIGN KEY (avatar_file_id) REFERENCES `avatar_file` (id) ON DELETE RESTRICT;

CREATE TABLE `session` (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id BIGINT UNSIGNED NOT NULL,
    token_hash BINARY(32) NOT NULL,
    auth_version BIGINT UNSIGNED NOT NULL,
    ttl_days SMALLINT UNSIGNED NOT NULL,
    created_at DATETIME(3) NOT NULL,
    last_seen_at DATETIME(3) NOT NULL,
    expires_at DATETIME(3) NOT NULL,
    revoked_at DATETIME(3) NULL,
    ip_address VARCHAR(45) CHARACTER SET ascii COLLATE ascii_bin NULL,
    user_agent VARCHAR(512) NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uq_session_token_hash (token_hash),
    KEY idx_session_user_validity (user_id, revoked_at, expires_at),
    KEY idx_session_expiry (expires_at, id),
    CONSTRAINT fk_session_user FOREIGN KEY (user_id) REFERENCES `user` (id) ON DELETE CASCADE,
    CONSTRAINT ck_session_auth_version CHECK (auth_version >= 1),
    CONSTRAINT ck_session_ttl_days CHECK (ttl_days BETWEEN 1 AND 30),
    CONSTRAINT ck_session_seen CHECK (last_seen_at >= created_at),
    CONSTRAINT ck_session_expiry CHECK (expires_at > last_seen_at)
) ENGINE=InnoDB DEFAULT CHARACTER SET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `system_setting` (
    `name` VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `value` JSON NOT NULL,
    row_version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    created_at DATETIME(3) NOT NULL,
    updated_at DATETIME(3) NOT NULL,
    PRIMARY KEY (`name`),
    CONSTRAINT ck_system_setting_name_length CHECK (CHAR_LENGTH(`name`) BETWEEN 3 AND 96),
    CONSTRAINT ck_system_setting_version CHECK (row_version >= 1)
) ENGINE=InnoDB DEFAULT CHARACTER SET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `system_instance` (
    id TINYINT UNSIGNED NOT NULL,
    installation_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    founder_user_id BIGINT UNSIGNED NOT NULL,
    key_fingerprint BINARY(32) NOT NULL,
    installed_at DATETIME(3) NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uq_system_instance_installation (installation_id),
    KEY idx_system_instance_founder (founder_user_id),
    CONSTRAINT fk_system_instance_founder FOREIGN KEY (founder_user_id) REFERENCES `user` (id) ON DELETE RESTRICT,
    CONSTRAINT ck_system_instance_singleton CHECK (id = 1)
) ENGINE=InnoDB DEFAULT CHARACTER SET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `audit_event` (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    actor_user_id BIGINT UNSIGNED NULL,
    actor_type VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    actor_name VARCHAR(64) NOT NULL,
    actor_username VARCHAR(30) CHARACTER SET ascii COLLATE ascii_bin NULL,
    action VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    target_type VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    target_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    target_label VARCHAR(160) NOT NULL,
    occurred_at DATETIME(3) NOT NULL,
    request_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NULL,
    ip_address VARCHAR(45) CHARACTER SET ascii COLLATE ascii_bin NULL,
    detail JSON NULL,
    PRIMARY KEY (id),
    KEY idx_audit_event_time (occurred_at, id),
    KEY idx_audit_event_actor_time (actor_user_id, occurred_at, id),
    KEY idx_audit_event_action_time (action, occurred_at, id),
    KEY idx_audit_event_target_time (target_type, target_id, occurred_at, id),
    CONSTRAINT fk_audit_event_actor FOREIGN KEY (actor_user_id) REFERENCES `user` (id) ON DELETE SET NULL,
    CONSTRAINT ck_audit_event_actor_type CHECK (actor_type IN ('user', 'system')),
    CONSTRAINT ck_audit_event_detail CHECK (detail IS NULL OR JSON_TYPE(detail) = 'OBJECT')
) ENGINE=InnoDB DEFAULT CHARACTER SET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
