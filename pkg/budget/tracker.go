package budget

import (
	"fmt"
	"sync"
)

type Tracker struct {
	maxBudget float64
	spent     float64
	mu        sync.Mutex
}

func NewTracker(maxBudget float64) *Tracker {
	return &Tracker{
		maxBudget: maxBudget,
	}
}

func (t *Tracker) AddCost(cost float64) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.spent += cost

	if t.maxBudget > 0 && t.spent >= t.maxBudget {
		return fmt.Errorf("budget limit reached: spent $%.2f of $%.2f", t.spent, t.maxBudget)
	}
	return nil
}

func (t *Tracker) Spent() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.spent
}

func (t *Tracker) Remaining() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.maxBudget <= 0 {
		return -1
	}
	return t.maxBudget - t.spent
}

func (t *Tracker) String() string {
	if t.maxBudget <= 0 {
		return fmt.Sprintf("$%.2f (unlimited)", t.Spent())
	}
	return fmt.Sprintf("$%.2f / $%.2f", t.Spent(), t.maxBudget)
}
