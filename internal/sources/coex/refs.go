package coex

import "github.com/smpain/event-intelligence-api/internal/sources"

type refCollector struct {
	refs  []sources.Ref
	index map[string]int
}

func newRefCollector() *refCollector {
	return &refCollector{index: make(map[string]int)}
}

func (c *refCollector) append(refs []sources.Ref) {
	for _, ref := range refs {
		if _, dup := c.index[ref.EventID]; dup {
			continue
		}
		c.index[ref.EventID] = len(c.refs)
		c.refs = append(c.refs, ref)
	}
}
