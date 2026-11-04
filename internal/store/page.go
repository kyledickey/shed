package store

// pageQuery completes selectFrom, a SELECT from table, into a query for the
// rows matching scope, newest first by (created_at, rowid), that come after
// the row with ID before, or from the newest for an empty before. A before
// that names no row matching scope selects none. A limit of zero or less
// selects every row. scopeArgs are the arguments of scope.
func pageQuery(selectFrom, table, scope string, scopeArgs []any, before string, limit int) (string, []any) {
	q := selectFrom + ` WHERE ` + scope
	args := append([]any(nil), scopeArgs...)
	if before != "" {
		q += ` AND (created_at, rowid) < (SELECT created_at, rowid FROM ` + table +
			` WHERE id = ? AND ` + scope + `)`
		args = append(args, before)
		args = append(args, scopeArgs...)
	}
	if limit <= 0 {
		limit = -1 // SQLite: no limit.
	}
	q += ` ORDER BY created_at DESC, rowid DESC LIMIT ?`
	return q, append(args, limit)
}
