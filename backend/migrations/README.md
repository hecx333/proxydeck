# Migrations

This directory contains the SQLite schema assets that match the current runtime behavior.

## Purpose

- Provide a reviewable SQL baseline for deployment and acceptance checks
- Preserve the schema fixes that are currently applied by runtime initialization
- Make a future move to an explicit migration tool easier

## Current Files

- `001_initial_schema.sql`
  Baseline tables and indexes for the current application model
- `002_runtime_fixes.sql`
  Runtime-safe fixes that must remain aligned with the live schema, including:
  - the unique index on `proxy_nodes(protocol, host, port)`
  - backfills for nullable traffic and request counters

## Source of Truth

`backend/internal/db/db.go` is still the active runtime entrypoint for schema creation and repair.

These SQL files are reference assets that should stay aligned with:

- GORM `AutoMigrate`
- startup-time `db.Exec(...)` repair logic

If models or runtime repair statements change, update this directory in the same change.
