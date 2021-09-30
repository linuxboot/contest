// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package rdbms

import (
	"fmt"
	"github.com/google/go-safeweb/safesql"
	"github.com/linuxboot/contest/pkg/job"
	"github.com/linuxboot/contest/pkg/storage"
	"github.com/linuxboot/contest/pkg/types"
	"github.com/linuxboot/contest/pkg/xcontext"
)

func (r *RDBMS) ListJobs(_ xcontext.Context, query *storage.JobQuery) ([]types.JobID, error) {
	res := []types.JobID{}

	// Quoting SQL strings is hard. https://github.com/golang/go/issues/18478
	// For now, just disallow anything that is not [a-zA-Z0-9_-]
	if err := job.CheckTags(query.Tags, true /* allowInternal */); err != nil {
		return nil, err
	}

	// Job state is updated by framework events, ensure there aren't any pending.
	err := r.flushFrameworkEvents()
	if err != nil {
		return nil, fmt.Errorf("could not flush events before reading events: %v", err)
	}

	// Construct the query.
	parts := []safesql.TrustedSQLString{safesql.New("SELECT jobs.job_id FROM jobs")}
	qargs := []interface{}{}
	// Tag filtering uses joins, 1 per tag.
	for i := range query.Tags {
		parts = append(
			parts,
			safesql.TrustedSQLStringConcat(
				safesql.New("INNER JOIN job_tags jt"),
				safesql.NewFromUint64(uint64(i)),
				safesql.New(" ON jobs.job_id = jt"),
				safesql.NewFromUint64(uint64(i)),
				safesql.New(".job_id"),
				),
		)
	}
	var conds []safesql.TrustedSQLString
	if len(query.ServerID) > 0 {
		conds = append(conds, safesql.New("jobs.server_id = ?"))
		qargs = append(qargs, query.ServerID)
	}
	if len(query.States) > 0 {
		stst := make([]safesql.TrustedSQLString, len(query.States))
		for i, st := range query.States {
			stst[i] = safesql.New("?")
			qargs = append(qargs, st)
		}
		conds = append(
			conds,
			safesql.TrustedSQLStringConcat(
				safesql.New("jobs.state IN ("),
				safesql.TrustedSQLStringJoin(stst, safesql.New(", ")),
				safesql.New(")")),
			)
	}
	// Now the corresponding conditions.
	for i, tag := range query.Tags {
		conds = append(conds, safesql.TrustedSQLStringConcat(
			safesql.New("jt"),
			safesql.NewFromUint64(uint64(i)),
			safesql.New(".tag = ?")))
		qargs = append(qargs, tag)
	}
	if len(conds) > 0 {
		parts = append(parts, safesql.New("WHERE"), safesql.TrustedSQLStringJoin(conds, safesql.New(" AND ")))
	}
	/* Examples of the resulting queries:
	SELECT jobs.job_id FROM jobs
	SELECT jobs.job_id FROM jobs WHERE jobs.state IN (0)
	SELECT jobs.job_id FROM jobs WHERE jobs.state IN (2, 3)
	SELECT jobs.job_id FROM jobs INNER JOIN job_tags jt0 ON jobs.job_id = jt0.job_id WHERE jt0.tag = "tests"
	SELECT jobs.job_id FROM jobs INNER JOIN job_tags jt0 ON jobs.job_id = jt0.job_id INNER JOIN job_tags jt1 ON jobs.job_id = jt1.job_id WHERE jobs.state IN (2, 3, 4) AND jt0.tag = "tests" AND jt1.tag = "foo"
	*/
	parts = append(parts, safesql.New("ORDER BY jobs.job_id"))
	stmt := safesql.TrustedSQLStringJoin(parts, safesql.New(" "))

	rows, err := r.db.Query(stmt, qargs...)
	if err != nil {
		return nil, fmt.Errorf("could not list jobs in states (sql: %q): %w", stmt, err)
	}
	defer func() {
		_ = rows.Close()
	}()
	for rows.Next() {
		var jobID types.JobID
		if err := rows.Scan(&jobID); err != nil {
			return nil, fmt.Errorf("could not list jobs in states (sql: %q): %w", stmt, err)
		}
		res = append(res, jobID)
	}

	return res, nil
}
