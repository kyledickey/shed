package control

// Page sizes of the deployment and backup lists.
const (
	// DefaultPage is how many records a page holds when its limit is unset.
	DefaultPage = 10
	// MaxPage is the most records a page holds.
	MaxPage = 100
)

// Page selects a page of a list that is newest first.
type Page struct {
	// Before is the ID of the last record of the previous page, or empty
	// for the first page. An ID that is not in the list selects an empty
	// page.
	Before string
	// Limit is the most records the page holds: DefaultPage if zero or
	// less, and at most MaxPage.
	Limit int
}

func (pg Page) limit() int {
	if pg.Limit <= 0 {
		return DefaultPage
	}
	return min(pg.Limit, MaxPage)
}

// cut trims rows, fetched with a limit one over the page's, to the page's
// limit. It returns the ID of the page's last row, through id, if rows held
// more, or else "".
func cut[T any](rows []T, limit int, id func(T) string) ([]T, string) {
	if len(rows) <= limit {
		return rows, ""
	}
	rows = rows[:limit]
	return rows, id(rows[limit-1])
}
