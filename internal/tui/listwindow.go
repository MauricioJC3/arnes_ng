package tui

// listWindow returns the [top, end) slice of an n-item list to render so that
// item idx stays on screen, scrolling once n exceeds maxRows. With maxRows >= n
// the whole list shows. Every scrollable picker (/model, /connect's model step,
// the "/" command menu) funnels through this so the selected row and its ❯
// never leave the screen in either direction.
func listWindow(idx, n, maxRows int) (top, end int) {
	if maxRows < 1 {
		maxRows = 1
	}
	if maxRows >= n {
		return 0, n
	}
	top = idx - maxRows/2
	if top < 0 {
		top = 0
	}
	if top > n-maxRows {
		top = n - maxRows
	}
	return top, top + maxRows
}
