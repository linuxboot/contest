-- Copyright (c) Facebook, Inc. and its affiliates.
--
-- This source code is licensed under the MIT license found in the
-- LICENSE file in the root directory of this source tree.

-- +goose Up

ALTER TABLE test_events ADD COLUMN test_attempt INTEGER UNSIGNED NOT NULL DEFAULT 0;

-- +goose Down

ALTER TABLE test_events DROP COLUMN test_attempt;